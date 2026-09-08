// Real Chromium browser flows with Node's standard library and Chrome DevTools Protocol.
// No npm packages, user browser profile, external endpoints or workstation adapters.
import {spawn, execFileSync} from 'node:child_process';
import {mkdtemp, readFile, realpath, rm, access} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import net from 'node:net';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const chrome=process.env.CHROME_BIN||(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':'/usr/bin/google-chrome');
try{await access(chrome);}catch{console.error('NOT RUN — compatible Chrome unavailable; set CHROME_BIN to an installed browser executable.');process.exit(77);}
const temporary=await realpath(await mkdtemp(path.join(tmpdir(),'bridge-browser-')));
let server,browser,cdp,secret='';
const delay=(ms)=>new Promise(resolve=>setTimeout(resolve,ms));
const run=(binary,args)=>execFileSync(binary,args,{cwd:root,encoding:'utf8',stdio:['ignore','pipe','pipe'],timeout:120000});
async function freePort(){const socket=net.createServer();await new Promise((resolve,reject)=>{socket.once('error',reject);socket.listen(0,'127.0.0.1',resolve);});const port=socket.address().port;await new Promise(resolve=>socket.close(resolve));return port;}
async function stop(process,signal='SIGTERM'){if(!process||process.exitCode!==null||process.signalCode!==null)return;const exited=new Promise(resolve=>process.once('exit',resolve));process.kill(signal);await Promise.race([exited,delay(5000)]);if(process.exitCode===null&&process.signalCode===null){process.kill('SIGKILL');await exited;}}
class CDP {
  constructor(ws){this.ws=ws;this.id=0;this.pending=new Map();ws.addEventListener('message',({data})=>{const message=JSON.parse(data);if(message.id){const p=this.pending.get(message.id);if(p){this.pending.delete(message.id);message.error?p.reject(new Error('Browser protocol command failed')):p.resolve(message.result);}}});}
  send(method,params={},sessionId){return new Promise((resolve,reject)=>{const id=++this.id;this.pending.set(id,{resolve,reject});this.ws.send(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})}));});}
}
try{
  const serverBin=path.join(temporary,'bridged'),cliBin=path.join(temporary,'bridgectl');
  run('go',['build','-o',serverBin,'./cmd/bridged']);run('go',['build','-o',cliBin,'./cmd/bridgectl']);
  const port=await freePort(),origin=`http://127.0.0.1:${port}`,demo=path.join(temporary,'demo');
  run(serverBin,['--init-demo',demo,'--demo-port',String(port)]);
  const policy=path.join(demo,'bridge.json'),credential=path.join(demo,'owner.token');
  run(cliBin,['admin','bootstrap','--config',policy,'--output',credential]);
  secret=(await readFile(credential,'utf8')).trim();
  const start=()=>spawn(serverBin,['--config',policy],{stdio:'ignore'});
  async function serverReady(){for(let i=0;i<100;i++){try{const r=await fetch(origin+'/health/live',{signal:AbortSignal.timeout(200)});if(r.ok)return;}catch{}await delay(30);}throw new Error('Fixture daemon did not become ready');}
  server=start();await serverReady();
  const chromeProfile=path.join(temporary,'chrome');
  browser=spawn(chrome,['--headless=new','--no-first-run','--no-default-browser-check','--disable-background-networking','--disable-component-update','--disable-extensions','--disable-sync','--remote-debugging-address=127.0.0.1','--remote-debugging-port=0',`--user-data-dir=${chromeProfile}`,'about:blank'],{stdio:'ignore'});
  let endpoint;
  for(let i=0;i<200;i++){try{const [debugPort,debugPath]=(await readFile(path.join(chromeProfile,'DevToolsActivePort'),'utf8')).trim().split('\n');endpoint=`ws://127.0.0.1:${debugPort}${debugPath}`;break;}catch{}await delay(25);}
  assert.ok(endpoint,'Chrome did not publish its isolated loopback debugging endpoint');
  const ws=new WebSocket(endpoint);await new Promise((resolve,reject)=>{ws.addEventListener('open',resolve,{once:true});ws.addEventListener('error',reject,{once:true});});cdp=new CDP(ws);
  const version=await cdp.send('Browser.getVersion');
  const target=await cdp.send('Target.createTarget',{url:'about:blank'}),attached=await cdp.send('Target.attachToTarget',{targetId:target.targetId,flatten:true}),session=attached.sessionId;
  const call=(method,params)=>cdp.send(method,params,session);
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true,userGesture:true});if(r.exceptionDetails)throw new Error('Browser script evaluation failed');return r.result.value;};
  async function waitFor(expression,description,timeout=10000){const until=Date.now()+timeout;while(Date.now()<until){if(await evaluate(expression))return;await delay(30);}throw new Error('Browser timeout: '+description);}
  async function navigate(page){await evaluate(`location.hash=${JSON.stringify(page)}`);await waitFor(`document.getElementById('page-title').textContent===${JSON.stringify({serving:'Models & serving',resources:'Resource budgets',profiles:'Operating profiles',builds:'Build jobs',caches:'Persistent assets',harnesses:'Developer clients',operations:'Operations & recovery'}[page])}`,'page '+page);}
  await call('Page.enable',{});await call('Runtime.enable',{});await call('Page.navigate',{url:origin});
  await waitFor(`document.getElementById('login') && !document.getElementById('login').hidden`,'sign-in form');
  await evaluate(`document.getElementById('credential').value=${JSON.stringify(secret)};document.getElementById('login-form').requestSubmit();`);
  await waitFor(`document.getElementById('mode').textContent.includes('DEMO') && !!document.getElementById('serving-form')`,'authenticated serving page');
  assert.equal(await evaluate(`document.getElementById('credential').value`),'');
  assert.equal(await evaluate(`localStorage.length`),0);
  assert.equal(await evaluate(`document.cookie.includes('bridge_session')`),false,'session cookie must be HttpOnly');
  console.log('PASS browser: credential exchange, HttpOnly session, isolated demo label');

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
  const operationPoll=async()=>{for(let i=0;i<200;i++){const response=await fetch(origin+'/api/v1/operations',{headers:{Authorization:'Bearer '+secret}});const ops=await response.json();const op=ops.find(o=>o.plan.draft.profile==='ai'&&o.state==='running'&&o.dispatched===true);if(op){await stop(server,'SIGKILL');return op.id;}await delay(2);}throw new Error('Could not observe the isolated operation dispatch boundary');};
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
  console.log(`PASS browser: narrow viewport and logout revocation (${version.product}; Node ${process.version})`);
}catch(error){console.error(String(error.message).replaceAll(secret||'__absent_credential__','[redacted]'));process.exitCode=1;}
finally{cdp?.ws.close();await stop(browser);await stop(server);await rm(temporary,{recursive:true,force:true});}
