// Real Chromium browser flows with Node's standard library and Chrome DevTools Protocol.
// No npm packages, user browser profile, external endpoints or workstation adapters.
import {spawn, execFileSync} from 'node:child_process';
import {mkdtemp, readFile, realpath, rm, access} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import net from 'node:net';
import https from 'node:https';
import {createHash, X509Certificate} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const chrome=process.env.CHROME_BIN||(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':'/usr/bin/google-chrome');
try{await access(chrome);}catch{console.error('NOT RUN — compatible Chrome unavailable; set CHROME_BIN to an installed browser executable.');process.exit(77);}
const temporary=await realpath(await mkdtemp(path.join(tmpdir(),'bridge-browser-')));
let server,browser,cdp,unrelated,secret='';
const delay=(ms)=>new Promise(resolve=>setTimeout(resolve,ms));
const run=(binary,args)=>execFileSync(binary,args,{cwd:root,encoding:'utf8',stdio:['ignore','pipe','pipe'],timeout:120000});
async function freePort(){const socket=net.createServer();await new Promise((resolve,reject)=>{socket.once('error',reject);socket.listen(0,'127.0.0.1',resolve);});const port=socket.address().port;await new Promise(resolve=>socket.close(resolve));return port;}
async function stop(process,signal='SIGTERM'){if(!process||process.exitCode!==null||process.signalCode!==null)return;const exited=new Promise(resolve=>process.once('exit',resolve));process.kill(signal);await Promise.race([exited,delay(5000)]);if(process.exitCode===null&&process.signalCode===null){process.kill('SIGKILL');await exited;}}
async function close(server){if(!server)return;server.closeAllConnections?.();await new Promise(resolve=>server.close(resolve));}
class CDP {
  constructor(ws){this.ws=ws;this.id=0;this.pending=new Map();ws.addEventListener('message',({data})=>{const message=JSON.parse(data);if(message.id){const p=this.pending.get(message.id);if(p){this.pending.delete(message.id);message.error?p.reject(new Error(`Browser protocol ${p.method} failed: ${message.error.message||'unknown error'}`)):p.resolve(message.result);}}});}
  send(method,params={},sessionId){return new Promise((resolve,reject)=>{const id=++this.id;this.pending.set(id,{resolve,reject,method});this.ws.send(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})}));});}
}
try{
  const serverBin=path.join(temporary,'bridge-browser-fixture');
  // This test-only build deliberately puts a live API cookie policy over the
  // Demo adapter. Production bridged has no corresponding flag or fallback.
  run('go',['build','-tags','bridge_browser_fixture','-o',serverBin,'./cmd/bridge-browser-fixture']);
  const port=await freePort(),managementHost=`bridge.test:${port}`,origin=`https://${managementHost}`,demo=path.join(temporary,'demo');
  const credential=path.join(demo,'owner.token');
  const certificate=path.join(temporary,'browser-testcert.pem'),privateKey=path.join(temporary,'browser-testkey.pem');
  run('go',['run','./scripts/browser-testcert.go','--host','bridge.test,untrusted.test','--cert',certificate,'--key',privateKey]);
  const certificatePEM=await readFile(certificate,'utf8');
  const privateKeyPEM=await readFile(privateKey,'utf8');
  const spki=createHash('sha256').update(new X509Certificate(certificatePEM).publicKey.export({type:'spki',format:'der'})).digest('base64');
  const start=()=>spawn(serverBin,['--state',demo,'--listen',`127.0.0.1:${port}`,'--origin',origin,'--cert',certificate,'--key',privateKey,'--credential',credential],{stdio:'ignore'});
  async function serverReady(){for(let i=0;i<100;i++){try{const ready=await new Promise(resolve=>{const request=https.get({hostname:'127.0.0.1',port,servername:'bridge.test',ca:certificatePEM,headers:{Host:managementHost},timeout:200},response=>{response.resume();resolve(response.statusCode===200);});request.on('error',()=>resolve(false));request.on('timeout',()=>{request.destroy();resolve(false);});});if(ready)return;}catch{}await delay(30);}throw new Error('Fixture daemon did not become ready with its hostname-bound test certificate');}
  async function apiJSON(endpoint,credential){return await new Promise((resolve,reject)=>{const request=https.get({hostname:'127.0.0.1',port,path:endpoint,servername:'bridge.test',ca:certificatePEM,headers:{Host:managementHost,Authorization:`Bearer ${credential}`},timeout:2000},response=>{let body='';response.setEncoding('utf8');response.on('data',chunk=>body+=chunk);response.on('end',()=>{if(response.statusCode!==200)return reject(new Error(`management API status ${response.statusCode}`));try{resolve(JSON.parse(body));}catch(error){reject(error);}});});request.on('error',reject);request.on('timeout',()=>{request.destroy();reject(new Error('management API timeout'));});});}
  server=start();await serverReady();secret=(await readFile(credential,'utf8')).trim();
  const chromeProfile=path.join(temporary,'chrome');
  browser=spawn(chrome,['--headless=new','--no-first-run','--no-default-browser-check','--disable-background-networking','--disable-component-update','--disable-extensions','--disable-sync','--host-resolver-rules=MAP bridge.test 127.0.0.1, MAP untrusted.test 127.0.0.1',`--ignore-certificate-errors-spki-list=${spki}`,'--remote-debugging-address=127.0.0.1','--remote-debugging-port=0',`--user-data-dir=${chromeProfile}`,'about:blank'],{stdio:'ignore'});
  let endpoint;
  for(let i=0;i<200;i++){try{const [debugPort,debugPath]=(await readFile(path.join(chromeProfile,'DevToolsActivePort'),'utf8')).trim().split('\n');endpoint=`ws://127.0.0.1:${debugPort}${debugPath}`;break;}catch{}await delay(25);}
  assert.ok(endpoint,'Chrome did not publish its isolated loopback debugging endpoint');
  const ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener('open',resolve,{once:true});ws.addEventListener('error',reject,{once:true});});cdp=new CDP(ws);
  const version=await cdp.send('Browser.getVersion');
  const target=await cdp.send('Target.createTarget',{url:'about:blank'}),attached=await cdp.send('Target.attachToTarget',{targetId:target.targetId,flatten:true}),session=attached.sessionId;
  const call=(method,params)=>cdp.send(method,params,session);
  let browserStep='';
  const evaluate=async (expression,description='')=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true,userGesture:true});if(r.exceptionDetails)throw new Error('Browser script evaluation failed'+(description||browserStep?': '+(description||browserStep):''));return r.result.value;};
  async function waitFor(expression,description,timeout=10000){const until=Date.now()+timeout;while(Date.now()<until){if(await evaluate(expression,description))return;await delay(30);}throw new Error('Browser timeout: '+description);}
  async function navigate(page){await evaluate(`location.hash=${JSON.stringify(page)}`);await waitFor(`document.getElementById('page-title').textContent===${JSON.stringify({serving:'Models & serving',resources:'Resource budgets',profiles:'Operating profiles',builds:'Build jobs',caches:'Persistent assets',harnesses:'Developer clients',operations:'Operations & recovery'}[page])}`,'page '+page);}
  await call('Page.enable',{});await call('Runtime.enable',{});await call('Page.navigate',{url:origin});
  await waitFor(`document.getElementById('login') && !document.getElementById('login').hidden`,'sign-in form');
  await evaluate(`document.getElementById('credential').value=${JSON.stringify(secret)};document.getElementById('login-form').requestSubmit();`);
  await waitFor(`document.getElementById('mode').textContent.includes('DEMO') && !!document.getElementById('serving-form')`,'authenticated serving page');
  assert.equal(await evaluate(`document.getElementById('credential').value`),'');
  assert.equal(await evaluate(`localStorage.length`),0);
  assert.equal(await evaluate(`document.cookie.includes('__Host-bridge_session_v2')`),false,'session cookie must be HttpOnly');
  const browserCookies=await call('Network.getAllCookies');
  const browserCookie=browserCookies.cookies.find(cookie=>cookie.name==='__Host-bridge_session_v2');
  assert.ok(browserCookie?.secure&&browserCookie.httpOnly&&browserCookie.sameSite==='Strict'&&browserCookie.path==='/',`unsafe HTTPS browser cookie: ${JSON.stringify(browserCookie)}`);
  const noCSRF=await evaluate(`fetch('/api/v1/auth/logout',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'}}).then(response=>response.status)`);
  assert.equal(noCSRF,403,'browser logout without CSRF was accepted');
  assert.equal(await evaluate(`fetch('/api/v1/auth/session',{credentials:'same-origin'}).then(response=>response.status)`),200,'CSRF refusal revoked the live session');
  let receivedManagementCookie=false,observedUnrelated;
  const unrelatedObserved=new Promise((resolve,reject)=>{observedUnrelated={resolve,reject};});
  unrelated=https.createServer({key:privateKeyPEM,cert:certificatePEM},(request,response)=>{receivedManagementCookie=request.headers.cookie?.includes('__Host-bridge_session_v2=')||false;response.end('untrusted loopback service');observedUnrelated.resolve();});
  const unrelatedPort=await new Promise((resolve,reject)=>{unrelated.once('error',reject);unrelated.listen(0,'127.0.0.1',()=>resolve(unrelated.address().port));});
  await call('Page.navigate',{url:`https://untrusted.test:${unrelatedPort}/`});
  await Promise.race([unrelatedObserved,delay(3000).then(()=>{throw new Error('unrelated loopback service did not receive browser navigation');})]);
  assert.equal(receivedManagementCookie,false,'management cookie reached unrelated loopback service');
  await call('Page.navigate',{url:origin});await waitFor(`document.getElementById('mode').textContent.includes('DEMO') && !!document.getElementById('serving-form')`,'secure session reconnect after loopback isolation check');
  console.log('PASS browser: live API cookie policy over isolated Demo adapter, narrow HTTPS test-certificate trust, HttpOnly session and unrelated loopback identity isolation');

  // Server-side validation appears in the real form. No duplicated JS budget rules.
  await evaluate(`document.getElementById('f-max_output_tokens').value='128';document.getElementById('serving-form').requestSubmit();`);
  await waitFor(`document.getElementById('notice').textContent.includes('unsupported')`,'unsupported serving cap');
  assert.equal(await evaluate(`document.getElementById('plan-dialog').open`),false);
  await evaluate(`document.getElementById('f-max_output_tokens').value='0';document.getElementById('f-concurrency').value='3';document.getElementById('serving-form').requestSubmit();`);
  await waitFor(`document.getElementById('plan-dialog').open`,'exact serving plan');
  assert.ok(await evaluate(`document.getElementById('plan-summary').textContent.includes('serving.concurrency')`));
  await evaluate(`document.getElementById('target-confirm').value='wrong-target';document.getElementById('apply-form').requestSubmit();`);
  await waitFor(`document.getElementById('notice').textContent.includes('exact target')`,'exact target confirmation');
  async function applyPlan(){await evaluate(`document.getElementById('target-confirm').value='demo-workstation';document.getElementById('apply-form').requestSubmit();`);await waitFor(`!document.getElementById('plan-dialog').open`,'accepted operation');}
  await applyPlan();await waitFor(`document.getElementById('content').textContent.includes('succeeded')`,'successful serving apply');
  assert.ok(await evaluate(`document.getElementById('content').textContent.includes('Source updated: yes')`));
  console.log('PASS browser: validation refusal, exact preview and target, apply, separate source/live outcome');

  // Memory flows use only the Demo adapter's named synthetic evidence. A preview
  // never starts a workload; confirming the plan exports an unqualified fixture.
  browserStep='memory setup';
  await navigate('resources');
  await waitFor(`!!document.getElementById('memory-form')`,'owner memory form');
  const memoryRevision=await evaluate(`state.config.revision`);
  const memoryOperations=await evaluate(`state.operations.length`);
  async function selectMemory(id){await evaluate(`document.getElementById('memory-evidence-id').value=${JSON.stringify(id)};document.getElementById('memory-evidence-sha256').value='a'.repeat(64);document.getElementById('memory-other-mib').value='0';document.getElementById('memory-evidence-id').dispatchEvent(new Event('input',{bubbles:true}));`,'select memory fixture');}
  async function memoryButton(task){await evaluate(`document.querySelector('[data-memory-task="${task}"]').click()`,'memory button '+task);}
  await selectMemory('demo-incomplete');await memoryButton('preview-evidence');
  browserStep='incomplete memory preview';
  await waitFor(`document.getElementById('memory-preview').textContent.includes('incomplete')`,'incomplete memory evidence');
  assert.ok(await evaluate(`document.getElementById('memory-preview').textContent.includes('Startup and steady-state observations: unknown')`));
  assert.ok(await evaluate(`document.getElementById('memory-preview').textContent.includes('Shared-memory ceiling inside limit')`));
  await memoryButton('preview-candidate');
  await waitFor(`document.getElementById('memory-preview').textContent.includes('missing cold/warm')`,'missing cold/warm refusal');
  assert.equal(await evaluate(`document.getElementById('memory-export-plan').disabled`),true);
  assert.equal(await evaluate(`document.getElementById('plan-dialog').open`),false);
  await selectMemory('demo-refused');await memoryButton('preview-candidate');
  await waitFor(`document.getElementById('memory-preview').textContent.includes('PSI/OOM')`,'memory pressure refusal');
  assert.equal(await evaluate(`document.getElementById('memory-export-plan').disabled`),true);
  await selectMemory('demo-complete');await memoryButton('preview-candidate');
  browserStep='candidate preview';
  await waitFor(`document.getElementById('memory-preview').textContent.includes('plan-only-unqualified') && !document.getElementById('memory-export-plan').disabled`,'unqualified candidate preview');
  assert.equal(await evaluate(`state.operations.length`),memoryOperations,'memory preview dispatched an operation');
  assert.equal(await evaluate(`state.config.revision`),memoryRevision,'memory preview changed managed source');
  const observationMarkup=await evaluate(`memorySummary({...state.memoryPreview,observations:[{phase:'cold',pod_id:'synthetic-window',startup:{samples:3,duration_seconds:2,sampled_peak_bytes:1073741824,lifetime_peak_bytes:2147483648,shared_memory_bytes:0,host_available_min_bytes:4294967296},steady:{samples:0,duration_seconds:0,sampled_peak_bytes:0,lifetime_peak_bytes:0}}]})`,'memory window rendering');
  assert.ok(observationMarkup.includes('cold / startup')&&observationMarkup.includes('cold / steady')&&observationMarkup.includes('Sampled peak')&&observationMarkup.includes('Cgroup lifetime peak')&&observationMarkup.includes('unknown'),'window/peak distinctions or missing-data state absent');

  // Live preflight has not generated a candidate. Mock only that response; plan
  // creation still uses the isolated Demo owner API and must not apply anything.
  browserStep='memory export preflight fixture';
  await evaluate(`window.memoryPreflightSummary={...state.memoryPreview,status:'ready-for-plan',reason:'fixture_export_checks_passed',candidate_mib:0};window.memoryPreflightFetch=window.fetch;window.fetch=(input,options)=>input==='/api/v1/memory/preview'?Promise.resolve(new Response(JSON.stringify(window.memoryPreflightSummary),{status:200,headers:{'Content-Type':'application/json'}})):window.memoryPreflightFetch(input,options);`);
  await memoryButton('preview-candidate');
  await waitFor(`document.getElementById('memory-preview').textContent.includes('ready-for-plan') && !document.getElementById('memory-export-plan').disabled`,'live preflight enables export review');
  assert.ok(await evaluate(`document.getElementById('memory-preview').textContent.includes('Not generated; export checks passed') && document.getElementById('notice').textContent.includes('confirm the export plan')`),'preflight implied a generated candidate');
  await memoryButton('export');await waitFor(`document.getElementById('plan-dialog').open`,'preflight export awaits explicit review');
  assert.equal(await evaluate(`document.getElementById('target-confirm').value`),'');
  assert.equal(await evaluate(`state.operations.length`),memoryOperations,'preflight or review applied an operation');
  assert.equal(await evaluate(`state.config.revision`),memoryRevision,'preflight or review changed managed source');
  await evaluate(`window.fetch=window.memoryPreflightFetch;delete window.memoryPreflightFetch;delete window.memoryPreflightSummary;document.querySelector('#plan-dialog .dialog-header button').click();`);
  await memoryButton('preview-candidate');
  await waitFor(`state.memoryPreview?.status==='plan-only-unqualified' && !document.getElementById('memory-export-plan').disabled`,'Demo candidate restored after preflight fixture');

  await memoryButton('export');await waitFor(`document.getElementById('plan-dialog').open`,'memory export review');
  browserStep='memory export review';
  assert.ok(await evaluate(`document.getElementById('plan-summary').textContent.includes('does not change configuration or the running workload')`));
  assert.equal(await evaluate(`document.getElementById('target-confirm').value`),'');
  await applyPlan();
  browserStep='memory export completed';
  await waitFor(`state.operations.some(o=>o.plan.draft.action==='memory.plan.export'&&o.state==='succeeded')`,'memory fixture export');
  assert.equal(await evaluate(`state.config.revision`),memoryRevision,'memory export changed managed source');
  assert.ok(await evaluate(`state.operations.filter(o=>o.plan.draft.action==='memory.plan.export').every(o=>!o.source_updated&&!o.live_applied&&o.artifacts.some(a=>a.name==='memory-summary.json'&&JSON.parse(a.content).status==='plan-only-unqualified'))`));
  await navigate('resources');await waitFor(`document.getElementById('content').textContent.includes('Retained memory evidence') && state.memory.some(s=>s.status==='plan-only-unqualified')`,'retained memory summary');
  browserStep='disabled memory advisory';
  await memoryButton('advice');await waitFor(`document.getElementById('notice').textContent.includes('advisor is disabled')`,'disabled cloud adviser');
  assert.equal(await evaluate(`document.getElementById('plan-dialog').open`),false);

  // Browser-only advisory response fixture: the plan is created by the local
  // owner API, and no cloud request is made. Provider text cannot approve it.
  const beforeAdvisory=await evaluate(`state.operations.length`);
  browserStep='advisory response fixture';
  await evaluate(`(async()=>{window.memoryAdvisoryPlan=await api('plans','POST',{action:'memory.plan.export',target:state.inventory.target,source_revision:state.config.revision,memory:{evidence_id:'demo-complete',evidence_sha256:'a'.repeat(64),other_mib:0}});window.memoryOriginalFetch=window.fetch;window.fetch=(input,options)=>input==='/api/v1/memory/advice'?Promise.resolve(new Response(JSON.stringify({advisory:{summary:'<img src=x onerror="window.memoryInjected=true"> This text claims approval.'},plan:window.memoryAdvisoryPlan}),{status:200,headers:{'Content-Type':'application/json'}})):window.memoryOriginalFetch(input,options);})()`);
  await memoryButton('advice');await waitFor(`document.getElementById('plan-dialog').open`,'advisory plan awaits owner review');
  assert.equal(await evaluate(`document.getElementById('target-confirm').value`),'');
  assert.equal(await evaluate(`state.operations.length`),beforeAdvisory,'advisory response applied a plan');
  assert.ok(await evaluate(`document.getElementById('memory-advice').textContent.includes('Unapproved advisory explanation') && !document.getElementById('memory-advice').querySelector('img') && !window.memoryInjected`),'advisory content was interpreted as HTML or approval');
  assert.ok(await evaluate(`document.getElementById('memory-advice').textContent.includes('Token usage is unknown (not reported)')`),'missing provider accounting was presented as zero');
  await evaluate(`state.memoryAdvice.usage={input_tokens:10,output_tokens:5,total_tokens:15};state.memoryAdvice.usage_provisional=true;document.getElementById('memory-advice').innerHTML=renderMemoryAdvice();`);
  assert.ok(await evaluate(`document.getElementById('memory-advice').textContent.includes('Provisional token usage: 10 input, 5 output, 15 total. This is not a final bill.')`),'reported accounting lost its provisional label');
  await evaluate(`window.fetch=window.memoryOriginalFetch;delete window.memoryOriginalFetch;delete window.memoryAdvisoryPlan;document.querySelector('#plan-dialog .dialog-header button').click();`);
  console.log('PASS browser: memory unknown/incomplete/refused/candidate states, explicit export review, retained unqualified summary and advisory text without approval');
  browserStep='';

  for(const page of ['resources','builds','caches','harnesses'])await navigate(page);
  await navigate('profiles');
  await evaluate(`document.querySelector('[data-action="profile.switch"][data-extra*="gaming"]').click()`);
  await waitFor(`document.getElementById('plan-dialog').open`,'gaming plan');await applyPlan();
  await waitFor(`state.operations.some(o=>o.plan.draft.profile==='gaming' && o.state==='succeeded')`,'gaming operation');
  await call('Page.reload',{});await waitFor(`document.getElementById('mode').textContent.includes('DEMO') && !document.getElementById('management').hidden`,'session reconnect after refresh');
  await navigate('profiles');assert.ok(await evaluate(`document.getElementById('content').textContent.includes('Current profile: gaming')`));
  console.log('PASS browser: all management pages, gaming transition and session reconnect');

  // Kill only this isolated daemon after it durably records dispatch. This is a
  // process-crash recovery test; it does not claim a real GPU transition test.
  await evaluate(`document.querySelector('[data-action="profile.switch"][data-extra*="ai"]').click()`);
  await waitFor(`document.getElementById('plan-dialog').open`,'AI plan for crash boundary');
  const operationPoll=async()=>{for(let i=0;i<200;i++){const ops=await apiJSON('/api/v1/operations',secret);const op=ops.find(o=>o.plan.draft.profile==='ai'&&o.state==='running'&&o.dispatched===true);if(op){await stop(server,'SIGKILL');return op.id;}await delay(2);}throw new Error('Could not observe the isolated operation dispatch boundary');};
  const crashed=operationPoll();await evaluate(`document.getElementById('target-confirm').value='demo-workstation';document.getElementById('apply-form').requestSubmit();`);await crashed;
  server=start();await serverReady();await call('Page.reload',{});
  await waitFor(`document.getElementById('mode').textContent.includes('DEMO')`,'restarted session');await navigate('operations');
  await waitFor(`!!document.querySelector('[data-recover]')`,'recovery-required operation');
  await evaluate(`document.querySelector('[data-recover]').click()`);await waitFor(`document.getElementById('plan-dialog').open`,'explicit recovery plan');
  await applyPlan();await waitFor(`document.getElementById('content').textContent.includes('resolved-by-explicit-restore')`,'durable explicit restore');
  console.log('PASS browser: daemon crash, durable recovery-required, reviewed recovery and restore');

  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
  await navigate('resources');assert.ok(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+1`),'mobile layout must not overflow');
  await evaluate(`document.getElementById('logout').click()`);await waitFor(`!document.getElementById('login').hidden`,'logout');
  const result=await evaluate(`fetch('/api/v1/auth/session',{credentials:'same-origin'}).then(r=>r.status)`);assert.equal(result,401);
  await evaluate(`document.getElementById('credential').value=${JSON.stringify(secret)};document.getElementById('login-form').requestSubmit();`);
  await waitFor(`document.getElementById('mode').textContent.includes('DEMO') && !document.getElementById('management').hidden`,'second browser login');
  process.kill(server.pid,'SIGUSR1');
  await waitFor(`fetch('/api/v1/auth/session',{credentials:'same-origin'}).then(response=>response.status===401)`,'server-side browser session expiry');
  console.log(`PASS browser: narrow viewport, logout revocation, CSRF refusal and server-side expiry (${version.product}; Node ${process.version})`);
}catch(error){console.error(String(error.message).replaceAll(secret||'__absent_credential__','[redacted]'));process.exitCode=1;}
finally{cdp?.ws.close();await close(unrelated);await stop(browser);await stop(server);await rm(temporary,{recursive:true,force:true});}
