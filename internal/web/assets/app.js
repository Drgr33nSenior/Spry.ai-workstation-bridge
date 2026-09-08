'use strict';
const $ = (id) => document.getElementById(id);
const state = {session:null, inventory:null, config:null, operations:[], plan:null, applyKey:null, page:'serving', busy:false};
const pages = {serving:'Models & serving',resources:'Resource budgets',profiles:'Operating profiles',builds:'Build jobs',caches:'Persistent assets',harnesses:'Developer clients',operations:'Operations & recovery'};
const terminal = new Set(['succeeded','failed','cancelled','recovery-required']);
const escape = (v) => String(v ?? 'unknown').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const json = (v) => JSON.stringify(v,null,2);
const pretty = (v) => typeof v === 'object' ? json(v) : String(v ?? 'unknown');
const gib = (v) => Number.isFinite(v) && v>=0 ? `${(v/1073741824).toFixed(1)} GiB` : 'unknown';
function notice(message,error=false) { $('notice').textContent=message;$('notice').className=error?'error':''; }
function errorMessage(error) { return `${error.code ? error.code+': ' : ''}${error.message}`; }
async function api(path,method='GET',body=null,key=null,revision=null) {
  const headers={Accept:'application/json'};
  if(body!==null)headers['Content-Type']='application/json';
  if(method!=='GET' && state.session)headers['X-CSRF-Token']=state.session.csrf;
  if(key)headers['Idempotency-Key']=key;
  if(revision!==null)headers['If-Match']=`"${revision}"`;
  let response;
  try { response=await fetch('/api/v1/'+path,{method,headers,credentials:'same-origin',body:body===null?undefined:JSON.stringify(body),signal:AbortSignal.timeout(30000)}); }
  catch { throw new Error('Connection interrupted. Submitted work may still be running. Refresh Operations before trying again.'); }
  let result;try{result=await response.json();}catch{throw new Error('The server returned an unreadable response.');}
  if(!response.ok){const failure=result.error||result;const error=new Error(failure.message||'Request refused');error.code=failure.code;error.status=response.status;if(response.status===401 && path!=='auth/login')showLogin();throw error;}
  return result;
}
function showLogin(){state.session=null;$('login').hidden=false;$('management').hidden=true;$('logout').hidden=true;$('identity').textContent='';}
function showManagement(session){state.session=session;$('login').hidden=true;$('management').hidden=false;$('logout').hidden=false;const actor=session.actor;$('identity').textContent=typeof actor==='string'?actor:((actor?.name||actor?.id||'')+' · '+(actor?.role||''));}
async function refresh(render=true){
  $('connection').textContent='Refreshing…';
  try{
    const [inventory,config,operations]=await Promise.all([api('status'),api('config'),api('operations')]);
    state.inventory=inventory;state.config=config;state.operations=operations;
    $('mode').textContent=inventory.mode==='demo'?'DEMO · ISOLATED FIXTURES':`${inventory.environment} · ${inventory.target}`;
    $('mode').className=inventory.mode==='demo'?'demo':'';
    $('connection').textContent=inventory.cluster_available?'Connected':'K3s unavailable';
    if(render)renderPage();
  }catch(error){$('connection').textContent='Disconnected';notice(errorMessage(error),true);throw error;}
}
function detail(title,value){return `<details><summary>${escape(title)}</summary><pre>${escape(json(value))}</pre></details>`;}
function field(name,label,value,type='number',hint='') {return `<div><label for="f-${name}">${escape(label)}</label><input id="f-${name}" name="${name}" type="${type}" value="${escape(value)}" ${type==='number'?'step="any"':''} required>${hint?`<small>${escape(hint)}</small>`:''}</div>`;}
function action(label,action,extra={}){return `<button type="button" data-action="${escape(action)}" data-extra="${escape(JSON.stringify(extra))}">${escape(label)}</button>`;}
function modelOptions(){return state.inventory.models.map(m=>`<option value="${escape(m.id)}" ${m.id===state.config.serving.model?'selected':''}>${escape(m.id)} · ${escape(m.quantization)}</option>`).join('');}
function renderServing(){const s=state.config.serving;return `<section class="panel"><h2>Serving configuration</h2><p class="muted">Preview source changes and live effects before applying. The server validates the pinned engine and qualification gates.</p><form id="serving-form"><div class="form-grid"><div><label for="model">Reviewed model</label><select id="model" name="model">${modelOptions()}</select></div>${field('context','Context tokens',s.context)}${field('concurrency','Concurrent requests',s.concurrency)}${field('memory_fraction','GPU memory fraction',s.memory_fraction)}${field('cpu_offload_gib','CPU offload (GiB)',s.cpu_offload_gib)}${field('gpu_count','Device-plugin GPU count',state.config.resources.gpu_count)}${field('max_request_tokens','Global input cap',s.max_request_tokens,'number','Zero = not enforced. The pinned deployment refuses unsupported global caps.')}${field('max_output_tokens','Global output cap',s.max_output_tokens,'number','Client request limits are exported separately.')}</div><div class="actions"><button type="submit">Preview serving changes</button></div></form><div class="actions">${action('Plan start','serving.start')}${action('Plan graceful stop','serving.stop')}${action('Plan restart','serving.restart')}</div></section><h2>Selected model assets</h2><div class="cards">${state.inventory.models.map(m=>`<article class="card"><h3>${escape(m.id)}</h3><span class="pill">${escape(m.status)}</span><dl><dt>Quantization</dt><dd>${escape(m.quantization)}</dd><dt>Size</dt><dd>${gib(m.size)}</dd><dt>Licence</dt><dd>${escape(m.license||'unknown')}</dd><dt>Qualification</dt><dd>${escape(m.qualification||'unknown')}</dd><dt>Revision</dt><dd><code>${escape(m.revision)}</code></dd></dl><div class="actions">${action('Plan staging','model.stage',{model:m.id})}${action('Plan verification','model.verify',{model:m.id})}</div>${detail('Files and integrity evidence',m.files)}</article>`).join('')}</div>`;}
function renderResources(){const h=state.inventory.hardware,r=state.config.resources;return `<section class="panel"><h2>Observed target evidence</h2><span class="pill">${escape(h.status)}</span><dl><dt>Observation</dt><dd>${escape(h.observed_at)} · boot <code>${escape(h.boot_id)}</code></dd><dt>Memory</dt><dd>${escape(h.memory_mib)} MiB · ${escape(h.dimms)} DIMMs · trained ${escape(h.trained_speed)} · channels ${escape(h.channels)}</dd><dt>CPU</dt><dd>${escape(h.cpu_threads)} threads · SMT width ${escape(h.smt_width)} · topology ${h.topology_known?'known':'unknown'}</dd><dt>Memory reserves</dt><dd>Host ${h.host_reserve_mib} + K3s ${h.kube_reserve_mib} + other workloads ${h.other_memory_mib} MiB</dd><dt>CPU policy</dt><dd>${escape(h.cpu_manager_policy)} · full-pcpus-only ${escape(h.full_pcpus_only)}</dd></dl><p class="warning">Two GPUs have separate memory. Device-plugin counts do not choose a physical card. DIMM count does not establish memory bandwidth.</p>${(h.warnings||[]).map(w=>`<p class="warning">${escape(w)}</p>`).join('')}<div class="actions">${action('Plan evidence refresh','hardware.refresh')}${action('Export CPU maintenance plan','cpu-policy.export')}</div>${detail('Complete hardware observation',h)}</section><div class="cards">${(h.gpus||[]).map(g=>`<div class="card"><h3>${escape(g.model)}</h3><p><code>${escape(g.id)}</code></p><p>${escape(g.render_path)} · ${escape(g.memory_mib)} MiB</p></div>`).join('')||'<p>No GPU observations available.</p>'}</div><section class="panel"><h2>Workload budget</h2><form id="resources-form"><div class="form-grid">${field('cpu','CPU threads',r.cpu)}${field('memory_mib','RAM (MiB)',r.memory_mib)}${field('shared_memory_mib','Shared memory (MiB)',r.shared_memory_mib)}${field('gpu_count','Device-plugin GPU count',r.gpu_count)}</div><div class="actions"><button type="submit">Preview resource changes</button></div></form><p class="muted">Requests and limits stay equal for Guaranteed QoS. Whole-core validation uses observed SMT topology. Host CPU policy changes require a separate reviewed maintenance procedure.</p></section>`;}
function renderProfiles(){return `<section class="panel"><h2>Current profile: ${escape(state.inventory.profile)}</h2><p class="warning">Gaming may unload all AI workloads. Handover requires complete GPU-holder observations and bounded termination. A failed transition can require explicit recovery.</p><p>Sunshine and Steam Remote Play remain the existing alternative session paths. Build inhibition is cooperative; already-running unrelated builds are not suspended.</p></section><div class="cards">${[['ai','AI serving','Restore qualified AI workloads after device ownership is clear.'],['gaming','Gaming session','Stop AI and verify GPU release before the configured gaming path starts.'],['maintenance','Maintenance','Inhibit cooperating builds and stop managed GPU workloads.']].map(([id,title,description])=>`<article class="card"><h2>${title}</h2><p>${description}</p>${action('Preview '+title,'profile.switch',{profile:id})}</article>`).join('')}</div><section class="panel"><h2>Restore previous session</h2><p>Restore uses the durable executor snapshot and rechecks current hardware and device holders.</p>${action('Preview restore','profile.restore')}</section>`;}
function renderBuilds(){const recipes=state.inventory.recipes||[];return `<p class="muted">Only reviewed recipes run in the contained unprivileged worker. Completion creates an artifact; it does not promote, install or qualify it.</p><div class="cards">${recipes.map(r=>`<article class="card"><h2>${escape(r.id)}</h2><span class="pill">${escape(r.status)}</span><p>${escape(r.reason||'Reviewed recipe')}</p><dl><dt>Source revision</dt><dd><code>${escape(r.revision)}</code></dd><dt>Output</dt><dd>${escape(r.output)}</dd><dt>Qualification</dt><dd>${escape(r.qualification)}</dd></dl>${action('Preview queued build','build.start',{recipe:r.id})}</article>`).join('')||'<p>No reviewed recipes are available.</p>'}</div><h2>Build jobs</h2>${operationTable(state.operations.filter(o=>o.plan.draft.action==='build.start'))}`;}
function renderCaches(){const b=state.config.caches;return `<div class="cards">${(state.inventory.caches||[]).map(c=>`<article class="card"><h2>${escape(c.name)}</h2><span class="pill">${escape(c.status)}</span><p>${c.status==='observed'||c.status.startsWith('DEMO')?gib(c.used_bytes):'unknown'} used / ${gib(c.budget_bytes)} budget</p>${detail('Bounded cleanup preview (no deletion)',c.cleanup_preview||[])}</article>`).join('')}</div><section class="panel"><h2>Persistent asset and compilation budgets</h2><form id="caches-form"><div class="form-grid">${field('models_gib','Model cache (GiB)',b.models_gib)}${field('compiler_gib','Compiler cache (GiB)',b.compiler_gib)}${field('shader_gib','Shader cache (GiB)',b.shader_gib)}${field('build_jobs','Compilation jobs',b.build_jobs)}${field('build_memory_mib','Build memory (MiB)',b.build_memory_mib)}${field('scratch_gib','Build scratch (GiB)',b.scratch_gib)}</div><div class="actions"><button type="submit">Preview budget changes</button></div></form><p class="muted">These budgets preserve shared storage and NAS backup ownership. There is no automatic model deletion or general directory cleanup.</p></section>`;}
function renderHarnesses(){return `<p>Manual preference: Qwen Code, then DSH, then Hermes. Export the native bundle and configure the client locally. Credentials stay in supported local secret inputs. RAG access is configured separately.</p><div class="cards">${(state.inventory.harnesses||[]).map(h=>`<article class="card"><h2>${escape(h.id)}</h2><dl><dt>Version</dt><dd><code>${escape(h.version)}</code></dd><dt>Modes</dt><dd>${escape((h.modes||[]).join(', '))}</dd></dl>${(h.limitations||[]).map(l=>`<p class="warning">${escape(l)}</p>`).join('')}<button data-export="${escape(h.id)}">Download native bundle</button><p class="muted"><code>bridgectl harness configure ${escape(h.id)} --directory ./client-profile</code></p></article>`).join('')}</div>`;}
function operationTable(ops) {
  if(!ops.length)return '<div class="panel"><p>No operations yet. Create and review a plan to start.</p></div>';
  return `<div class="table-wrap"><table><thead><tr><th>Operation / action</th><th>Progress</th><th>Durable outcome</th><th>Actions</th></tr></thead><tbody>${ops.map(o=>`<tr>
    <td><code>${escape(o.id)}</code><br>${escape(o.plan.draft.action)}<br><small>${escape(o.updated_at)}</small></td>
    <td><span class="pill">${escape(o.state)}</span><br>${escape(o.phase)}<br>${escape(o.message)}${detail('Progress, artifacts and provenance',o)}</td>
    <td>Source updated: ${o.source_updated?'yes':'no'}<br>Live applied: ${o.live_applied?'yes':'no'}${o.recovery_required?'<p class="warning">Recovery required. Inspect host executor state before applying the restore plan.</p>':''}</td>
    <td>${!terminal.has(o.state)?`<button data-cancel="${escape(o.id)}">Request cancellation</button>`:''}
    ${o.recovery_required?`<button data-recover="${escape(o.id)}">Preview recovery</button>`:''}
    ${(o.artifacts||[]).map((a,i)=>a.content?`<div class="actions"><button data-artifact="${escape(o.id)}" data-index="${i}">Download ${escape(a.name)}</button></div>`:'').join('')}</td>
    </tr>`).join('')}</tbody></table></div>`;
}
function renderOperations(){return `<p class="muted">Disconnecting or refreshing does not cancel work. Cancellation is a request; wait for a durable terminal outcome. Uncertain external effects stay recovery-required.</p>${operationTable(state.operations)}`;}
function renderPage(){if(!state.inventory)return;state.page=location.hash.slice(1);if(!pages[state.page])state.page='serving';$('page-title').textContent=pages[state.page];for(const a of document.querySelectorAll('[data-page]')){if(a.dataset.page===state.page)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');}const renderers={serving:renderServing,resources:renderResources,profiles:renderProfiles,builds:renderBuilds,caches:renderCaches,harnesses:renderHarnesses,operations:renderOperations};$('content').innerHTML=renderers[state.page]();bindForms();}
function formNumbers(form){return Object.fromEntries([...new FormData(form)].map(([k,v])=>[k,Number(v)]));}
function bindForms(){
  $('serving-form')?.addEventListener('submit',event=>{event.preventDefault();const f=event.currentTarget,values=formNumbers(f);delete values.model;const gpu=values.gpu_count;delete values.gpu_count;submitPlan('serving.configure',{serving:{...values,model:new FormData(f).get('model')},resources:{...state.config.resources,gpu_count:gpu}});});
  $('resources-form')?.addEventListener('submit',event=>{event.preventDefault();submitPlan('resources.configure',{resources:formNumbers(event.currentTarget)});});
  $('caches-form')?.addEventListener('submit',event=>{event.preventDefault();submitPlan('caches.configure',{caches:formNumbers(event.currentTarget)});});
}
async function submitPlan(action,extra={}){try{const draft={action,target:state.inventory.target,source_revision:state.config.revision,...extra};const plan=await api('plans','POST',draft);showPlan(plan);notice('Plan validated. Review consequences and the exact target before applying.');}catch(error){notice(errorMessage(error),true);}}
function showPlan(plan){state.plan=plan;state.applyKey=crypto.randomUUID();const preview=plan.preview;const changes=preview.changes||[];$('plan-summary').innerHTML=`<p><strong>${escape(plan.draft.action)}</strong> on <code>${escape(plan.draft.target)}</code></p><p class="muted">Expires ${escape(plan.expires_at)}</p>${changes.length?`<div class="table-wrap"><table><thead><tr><th>Setting</th><th>Current</th><th>Requested</th></tr></thead><tbody>${changes.map(c=>`<tr><th>${escape(c.field)}</th><td>${escape(pretty(c.before))}</td><td>${escape(pretty(c.after))}</td></tr>`).join('')}</tbody></table></div>`:'<p>This plan changes operational state without editing configuration.</p>'}${(preview.consequences||[]).map(c=>`<p class="warning">${escape(c)}</p>`).join('')}${(preview.warnings||[]).map(c=>`<p class="warning">${escape(c)}</p>`).join('')}`;$('plan-json').textContent=json(plan);$('target-confirm').value='';$('target-confirm').placeholder=plan.draft.target;$('apply-consequence').textContent='Apply records durable intent and can disrupt workloads. Source updates and live application have separate outcomes.';$('plan-dialog').showModal();$('target-confirm').focus();}
$('login-form').addEventListener('submit',async event=>{event.preventDefault();const button=event.currentTarget.querySelector('button');button.disabled=true;try{const session=await api('auth/login','POST',{credential:$('credential').value});$('credential').value='';showManagement(session);notice('Signed in.');await refresh();}catch(error){$('credential').value='';notice(errorMessage(error),true);}finally{button.disabled=false;}});
$('logout').addEventListener('click',async()=>{try{await api('auth/logout','POST',{});showLogin();notice('Signed out. The browser session is revoked.');}catch(error){notice(errorMessage(error),true);}});
$('refresh').addEventListener('click',()=>refresh().catch(()=>{}));
$('export-source').addEventListener('click',async()=>{try{const config=await api('config/export');const blob=new Blob([json(config)+'\n'],{type:'application/json'}),url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download='managed-source.json';a.click();URL.revokeObjectURL(url);notice('Managed source exported with its content revision. Apply changes through a reviewed plan.');}catch(error){notice(errorMessage(error),true);}});
$('apply-form').addEventListener('submit',async event=>{event.preventDefault();if($('target-confirm').value!==state.plan.draft.target){notice('Type the exact target shown in the plan.',true);return;}const button=$('apply-button');button.disabled=true;try{const operation=await api('operations','POST',{plan_id:state.plan.id,target:$('target-confirm').value},state.applyKey);$('plan-dialog').close();location.hash='operations';notice(`Operation ${operation.id} accepted. Refreshing does not cancel it.`);await refresh();}catch(error){notice(errorMessage(error),true);}finally{button.disabled=false;}});
$('content').addEventListener('click',async event=>{
  const button=event.target.closest('button');
  if(!button)return;
  if(button.dataset.action){await submitPlan(button.dataset.action,JSON.parse(button.dataset.extra));return;}
  button.disabled=true;
  try {
    if(button.dataset.cancel){
      const observed=state.operations.find(o=>o.id===button.dataset.cancel);
      await api(`operations/${button.dataset.cancel}/cancel`,'POST',{},null,observed.revision);
      notice('Cancellation requested. Await executor confirmation.');
      await refresh();
    }
    if(button.dataset.recover)showPlan(await api(`operations/${button.dataset.recover}/recover`,'POST',{}));
    if(button.dataset.export){
      const bundle=await api(`harnesses/${button.dataset.export}/export`);
      download(json(bundle)+'\n',`bridge-${button.dataset.export}-bundle.json`,'application/json');
      notice('Native bundle downloaded. Verify and configure it on the client with bridgectl; launch is client-local.');
    }
    if(button.dataset.artifact){
      const operation=await api(`operations/${button.dataset.artifact}`),artifact=operation.artifacts[Number(button.dataset.index)];
      if(!artifact?.content)throw new Error('Artifact content is unavailable; refresh operation details.');
      const bytes=new TextEncoder().encode(artifact.content);
      const hash=[...new Uint8Array(await crypto.subtle.digest('SHA-256',bytes))].map(b=>b.toString(16).padStart(2,'0')).join('');
      if(hash!==artifact.sha256||bytes.length!==artifact.size)throw new Error('Artifact size or SHA-256 verification failed.');
      download(artifact.content,artifact.name.replace(/[^A-Za-z0-9_.-]/g,'_'),'application/octet-stream');
      notice('Artifact downloaded and SHA-256 verified. Export does not apply a host maintenance change.');
    }
  }catch(error){notice(errorMessage(error),true);}finally{button.disabled=false;}
});
function download(content,name,type){
  const blob=new Blob([content],{type}),url=URL.createObjectURL(blob),link=document.createElement('a');
  link.href=url;link.download=name;link.click();URL.revokeObjectURL(url);
}
window.addEventListener('hashchange',renderPage);
document.addEventListener('visibilitychange',()=>{if(!document.hidden && state.session)refresh().catch(()=>{});});
setInterval(()=>{if(state.session && !document.hidden && state.page==='operations' && !state.busy && !$('plan-dialog').open){state.busy=true;refresh().catch(()=>{}).finally(()=>{state.busy=false;});}},3000);
(async()=>{try{showManagement(await api('auth/session'));await refresh();}catch(error){showLogin();if(error.status!==401)notice(errorMessage(error),true);}})();
