'use strict';
const $ = (id) => document.getElementById(id);
const state = {session:null, inventory:null, config:null, operations:[], plan:null, applyKey:null, page:'serving', busy:false, memory:[], memoryError:'', memorySelection:null, memoryPreview:null, memoryPreviewError:'', memoryAdvice:null, performanceSelection:null, performancePreview:null, performancePreviewError:''};
const pages = {serving:'Models & serving',resources:'Resource budgets',performance:'Performance evidence',profiles:'Operating profiles',builds:'Build jobs',caches:'Persistent assets',harnesses:'Developer clients',operations:'Operations & recovery'};
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
function showLogin(){state.session=null;state.memory=[];state.memorySelection=null;state.memoryPreview=null;state.memoryAdvice=null;state.performanceSelection=null;state.performancePreview=null;$('login').hidden=false;$('management').hidden=true;$('logout').hidden=true;$('identity').textContent='';}
function showManagement(session){state.session=session;$('login').hidden=true;$('management').hidden=false;$('logout').hidden=false;const actor=session.actor;$('identity').textContent=typeof actor==='string'?actor:((actor?.name||actor?.id||'')+' · '+(actor?.role||''));}
async function refresh(render=true){
  $('connection').textContent='Refreshing…';
  try{
    const memory=memoryOwner()?api('memory').then(data=>({data,error:''})).catch(error=>({data:[],error:errorMessage(error)})):Promise.resolve({data:[],error:''});
    const [inventory,config,operations,evidence]=await Promise.all([api('status'),api('config'),api('operations'),memory]);
    state.inventory=inventory;state.config=config;state.operations=operations;
    state.memory=evidence.data;state.memoryError=evidence.error;
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
function renderResources(){const h=state.inventory.hardware,r=state.config.resources;return `<section class="panel"><h2>Observed target evidence</h2><span class="pill">${escape(h.status)}</span><dl><dt>Observation</dt><dd>${escape(h.observed_at)} · boot <code>${escape(h.boot_id)}</code></dd><dt>Memory</dt><dd>${memoryMiB(h.memory_mib)} · ${escape(h.dimms)} DIMMs · trained ${escape(h.trained_speed)} · channels ${escape(h.channels)}</dd><dt>CPU</dt><dd>${escape(h.cpu_threads)} threads · SMT width ${escape(h.smt_width)} · topology ${h.topology_known?'known':'unknown'}</dd><dt>Memory reserves</dt><dd>Host ${escape(h.host_reserve_mib)} + K3s ${escape(h.kube_reserve_mib)} + other workloads ${escape(h.other_memory_mib)} MiB</dd><dt>CPU policy</dt><dd>${escape(h.cpu_manager_policy)} · full-pcpus-only ${escape(h.full_pcpus_only)}</dd></dl><p class="warning">Two GPUs have separate memory. Device-plugin counts do not choose a physical card. DIMM count does not establish memory bandwidth.</p>${(h.warnings||[]).map(w=>`<p class="warning">${escape(w)}</p>`).join('')}<div class="actions">${action('Plan evidence refresh','hardware.refresh')}${action('Export CPU maintenance plan','cpu-policy.export')}</div>${detail('Complete hardware observation',h)}</section><div class="cards">${(h.gpus||[]).map(g=>`<div class="card"><h3>${escape(g.model)}</h3><p><code>${escape(g.id)}</code></p><p>${escape(g.render_path)} · ${memoryMiB(g.memory_mib)}</p></div>`).join('')||'<p>No GPU observations available.</p>'}</div><section class="panel"><h2>Workload budget</h2><form id="resources-form"><div class="form-grid">${field('cpu','CPU threads',r.cpu)}${field('memory_mib','RAM request and limit (MiB)',r.memory_mib)}${field('shared_memory_mib','Shared-memory ceiling (MiB)',r.shared_memory_mib,'number','Shared memory is included inside the RAM limit.')}${field('gpu_count','Device-plugin GPU count',r.gpu_count)}</div><div class="actions"><button type="submit">Preview resource changes</button></div></form><p class="muted">Requests and limits stay equal for Guaranteed QoS. Whole-core validation uses observed SMT topology. Host CPU policy changes require a separate reviewed maintenance procedure.</p></section>${renderMemory()}`;}
function memoryOwner(){return state.session?.actor?.role==='owner';}
function memoryAction(action){return action==='memory.evidence.import'||action==='memory.plan.export';}
function memoryExportReady(){return ['ready-for-plan','plan-only-unqualified'].includes(state.memoryPreview?.status);}
function privateMemoryArtifact(name){return ['plan.json','patch.json','rollback.json'].includes(name);}
function memoryMiB(value){return Number.isFinite(value)&&value>0?`${escape(value)} MiB`:'unknown';}
function memoryPeak(value){return Number.isFinite(value)&&value>0?gib(value):'unknown';}
function memorySummaryBase(summary){
  const observations=summary.observations||[];
  const candidate=summary.candidate_mib>0?memoryMiB(summary.candidate_mib)+' · unqualified':summary.status==='ready-for-plan'?'Not generated; export checks passed':'No justified candidate';
  const rows=observations.flatMap(observation=>['startup','steady'].map(phase=>{
    const window=observation[phase]||{},measured=window.samples>0;
    return `<tr><td>${escape(observation.phase)} / ${phase}<br><small>${escape(observation.pod_id)}</small></td><td>${measured?escape(window.samples):'unknown'} / ${measured&&window.duration_seconds>0?escape(window.duration_seconds)+' s':'unknown'}</td><td>${measured?memoryPeak(window.sampled_peak_bytes):'unknown'}</td><td>${measured?memoryPeak(window.lifetime_peak_bytes):'unknown'}</td><td>${measured?gib(window.shared_memory_bytes):'unknown'}</td><td>${measured?memoryPeak(window.host_available_min_bytes):'unknown'}</td></tr>`;
  })).join('');
  return `<article class="card"><h3>${escape(summary.evidence_id)}</h3><span class="pill">${escape(summary.status)}</span><p>${escape(summary.reason)}</p><dl><dt>Configured request / limit</dt><dd>${memoryMiB(summary.baseline_mib)} / ${memoryMiB(summary.limited_mib)}</dd><dt>Shared-memory ceiling inside limit</dt><dd>${memoryMiB(summary.shared_memory_mib)}</dd><dt>Candidate request / limit</dt><dd>${candidate}</dd><dt>Observation envelope / headroom</dt><dd>${memoryPeak(summary.envelope_bytes)} / ${memoryPeak(summary.headroom_bytes)}</dd><dt>Cold / warm observations</dt><dd>${Number.isInteger(summary.cold)?escape(summary.cold):'unknown'} / ${Number.isInteger(summary.warm)?escape(summary.warm):'unknown'}</dd></dl><p class="warning">Evidence and exported candidates are unqualified. Missing cold/warm runs, startup/final samples, or refusal conditions prevent a justified reduction.</p>${rows?`<div class="table-wrap"><table><thead><tr><th>Run / window</th><th>Samples / duration</th><th>Sampled peak</th><th>Cgroup lifetime peak</th><th>Observed shared memory</th><th>Minimum host available</th></tr></thead><tbody>${rows}</tbody></table></div>`:'<p class="muted">Startup and steady-state observations: unknown. No measurement windows were supplied.</p>'}${(summary.limitations||[]).map(value=>`<p class="warning">${escape(value)}</p>`).join('')}${detail('Summary and evidence preconditions',summary)}</article>`;
}
function telemetryAllowance(summary){
  if(!Number.isInteger(summary.telemetry_calculated_allowance_mib)||summary.telemetry_calculated_allowance_mib<0)return '';
  const components=Object.entries(summary.telemetry_component_limits_mib||{}).map(([name,value])=>'<li>'+escape(name)+': '+memoryMiB(value)+'</li>').join('');
  return '<article class="card"><h3>Telemetry planning allowance</h3><dl><dt>Selected profile</dt><dd>'+escape(summary.telemetry_profile||'unknown')+'</dd><dt>Calculated allowance</dt><dd>'+memoryMiB(summary.telemetry_calculated_allowance_mib)+'</dd><dt>Configured reserve</dt><dd>'+memoryMiB(summary.telemetry_reserve_mib)+'</dd><dt>Explicit margin</dt><dd>'+memoryMiB(summary.telemetry_margin_mib)+'</dd></dl>'+(components?'<p class="muted">Component limits used for planning:</p><ul>'+components+'</ul>':'<p class="muted">Component limits are unknown.</p>')+'<p class="warning">This is a configured planning allowance, not measured telemetry RSS or a memory saving available to a workload.</p></article>';
}
function memorySummary(summary){return memorySummaryBase(summary)+telemetryAllowance(summary);}
function renderMemory(){
  if(!memoryOwner())return '<section class="panel"><h2>Memory evidence and budget plans</h2><p>Owner access is required to inspect sealed memory evidence and request export plans.</p></section>';
  const demo=state.inventory.mode==='demo';
  if(!state.memorySelection)state.memorySelection={evidence_id:demo?'demo-complete':'',evidence_sha256:demo?'0'.repeat(64):'',other_mib:state.inventory.hardware.other_memory_mib||0};
  const selected=state.memorySelection;
  return `<section class="panel"><h2>Memory evidence and budget plans</h2><p>Compare measured startup and steady-state usage with the configured request and limit. Shared memory is included inside the RAM limit.</p>${demo?'<p class="warning">DEMO: demo-complete, demo-incomplete and demo-refused are synthetic fixtures. No measured RAM saving, cloud call or workload qualification is demonstrated.</p>':''}<form id="memory-form"><div class="form-grid"><div><label for="memory-evidence-id">Sealed evidence ID</label><input id="memory-evidence-id" name="evidence_id" value="${escape(selected.evidence_id)}" pattern="[A-Za-z0-9][A-Za-z0-9-]{7,79}" required></div><div><label for="memory-evidence-sha256">Manifest SHA-256</label><input id="memory-evidence-sha256" name="evidence_sha256" value="${escape(selected.evidence_sha256)}" pattern="[a-f0-9]{64}" maxlength="64" required></div><div><label for="memory-other-mib">Other workload reserve (MiB)</label><input id="memory-other-mib" name="other_mib" type="number" min="0" max="1073741824" step="1" value="${escape(selected.other_mib)}" required></div></div><p class="muted">Use the ID and manifest digest of the sealed bundle. Import preserves the evidence. An exported plan requires owner review and does not change the running workload.</p><div class="actions"><button type="submit" data-memory-task="preview-evidence">Inspect selected evidence</button><button type="submit" data-memory-task="import">Review evidence import</button><button type="submit" data-memory-task="preview-candidate">Check candidate export</button><button id="memory-export-plan" type="submit" data-memory-task="export" ${memoryExportReady()?'':'disabled'}>Review candidate export</button></div><p class="muted">The optional OpenAI adviser sends the selected sanitized memory evidence to OpenAI when enabled by the owner. Its explanation cannot approve or apply a plan.</p><div class="actions"><button type="submit" data-memory-task="advice">Ask optional memory adviser</button></div></form><div id="memory-preview" aria-live="polite">${renderMemoryPreview()}</div><div id="memory-advice" aria-live="polite">${renderMemoryAdvice()}</div></section><section class="panel"><h2>Retained memory evidence</h2>${state.memoryError?`<p class="error">${escape(state.memoryError)}</p>`:''}${state.memory.length?state.memory.map(memorySummary).join(''):'<p>No imported memory summaries are available.</p>'}</section>`;
}
function renderMemoryPreview(){return state.memoryPreviewError?`<p class="error">${escape(state.memoryPreviewError)}</p>`:state.memoryPreview?memorySummary(state.memoryPreview):'';}
function renderMemoryAdvice(){if(!state.memoryAdvice)return '';const advice=state.memoryAdvice;const accounting=advice.usage==null?'Token usage is unknown (not reported). This is not a final bill.':`Provisional token usage: ${escape(advice.usage.input_tokens)} input, ${escape(advice.usage.output_tokens)} output, ${escape(advice.usage.total_tokens)} total. This is not a final bill.`;return `<h3>Unapproved advisory explanation</h3><pre>${escape(advice.summary)}</pre><p>${accounting}</p><p class="warning">The explanation is advisory text. Review the separately validated plan before confirming any export.</p>`;}
function memorySelection(form){const fields=new FormData(form);return {evidence_id:String(fields.get('evidence_id')),evidence_sha256:String(fields.get('evidence_sha256')),other_mib:Number(fields.get('other_mib'))};}
function clearMemoryPreview(){state.memoryPreview=null;state.memoryPreviewError='';state.memoryAdvice=null;if($('memory-preview'))$('memory-preview').innerHTML='';if($('memory-advice'))$('memory-advice').innerHTML='';if($('memory-export-plan'))$('memory-export-plan').disabled=true;}
async function memorySubmit(event){
  event.preventDefault();const form=event.currentTarget,button=event.submitter,task=button?.dataset.memoryTask||'preview-evidence';
  state.memorySelection=memorySelection(form);
  const selected={...state.memorySelection},action=task==='preview-evidence'||task==='import'?'memory.evidence.import':'memory.plan.export';
  const draft={action,target:state.inventory.target,source_revision:state.config.revision,memory:selected};
  const controls=[...form.querySelectorAll('button,input')];controls.forEach(control=>control.disabled=true);
  try{
    if(task==='import'||task==='export'){await submitPlan(action,{memory:selected});return;}
    clearMemoryPreview();
    if(task==='advice'){
      const response=await api('memory/advice','POST',selected);
      if(!memoryOwner()||json(state.memorySelection)!==json(selected))return;
      state.memoryAdvice=response.advisory;if($('memory-advice'))$('memory-advice').innerHTML=renderMemoryAdvice();
      if(response.plan){showPlan(response.plan);notice('Advisory returned a validated export plan for your review. Nothing has been approved or applied.');}
      else notice('Advisory explanation received. No plan has been approved or applied.');
    }else{
      const summary=await api('memory/preview','POST',draft);
      if(!memoryOwner()||json(state.memorySelection)!==json(selected))return;
      state.memoryPreview=summary;if($('memory-preview'))$('memory-preview').innerHTML=renderMemoryPreview();
      notice(task==='preview-candidate'?'Memory export checks received. Review and confirm the export plan to generate an unqualified candidate.':'Evidence inspected. Previewing does not import evidence or change the workload.');
    }
  }catch(error){state.memoryPreviewError=errorMessage(error);if($('memory-preview'))$('memory-preview').innerHTML=renderMemoryPreview();notice(errorMessage(error),true);}
  finally{controls.forEach(control=>control.disabled=false);if($('memory-export-plan'))$('memory-export-plan').disabled=!memoryExportReady();}
}
function performanceOwner(){return state.session?.actor?.role==='owner';}
function performanceAction(action){return action==='performance.export'||action==='performance.profile.select';}
const performanceKinds=['comparison','coding-eval','loading','queue','warm-status','cache','profile-selection','profile-status'];
function performanceActionFor(kind){return kind==='profile-selection'?'performance.profile.select':'performance.export';}
function performanceSummaryBase(summary){
  const fields=(summary.fields||[]).map(field=>'<tr><th>'+escape(field.name)+'</th><td>'+escape(field.value)+' '+escape(field.unit)+'</td><td>'+escape(field.state)+'</td></tr>').join('');
  return '<article class=\'card\'><h3>'+escape(summary.kind)+'</h3><span class=\'pill\'>'+escape(summary.status)+'</span><p>'+escape(summary.reason||'No reason was supplied.')+'</p><p class=\'muted\'>Observed '+escape(summary.observed_at||'unknown')+'. This is historical analysis, not the current serving lifecycle.</p>'+(fields?'<div class=\'table-wrap\'><table><thead><tr><th>Measurement</th><th>Value</th><th>State</th></tr></thead><tbody>'+fields+'</tbody></table></div>':'<p class=\'muted\'>No measurements were exported. Missing values are unknown, not zero.</p>')+(summary.limitations||[]).map(note=>'<p class=\'warning\'>'+escape(note)+'</p>').join('')+detail('Bounded provenance and preconditions',summary)+'</article>';
}
function performanceSummary(summary){
  const historical=summary.kind==='profile-status'?'<p class="warning">This is a selected historical profile report. It is not current workload qualification. Without fresh owner-observed identity evidence, status is unknown and cannot be treated as current.</p>':'';
  return performanceSummaryBase(summary)+historical;
}
function renderServingLifecycle(){
  const current=state.inventory?.serving_status;
  if(!current)return '<section class=\'panel\'><h2>Current serving lifecycle</h2><p class=\'muted\'>Unavailable: the current adapter has not supplied a safe lifecycle observation. Historical reports cannot establish readiness.</p></section>';
  const ready=current.kubernetes_ready===true?'ready':current.kubernetes_ready===false?'not ready':'unknown';
  return '<section class=\'panel\'><h2>Current serving lifecycle</h2><span class=\'pill\'>'+escape(current.state||'unknown')+'</span><dl><dt>Kubernetes readiness</dt><dd>'+ready+'</dd><dt>Representative warmup</dt><dd>'+escape(current.representative_warmup||'not applicable or unknown')+'</dd><dt>Observed</dt><dd>'+escape(current.observed_at||'unknown')+'</dd><dt>Identity</dt><dd><code>'+escape(current.identity||'unknown')+'</code></dd></dl><p class=\'warning\'>'+escape(current.reason||'Warm status is invalid after a restart or a relevant model, runtime, process, or device identity change.')+'</p></section>';
}
function renderPerformance(){
  const demo=state.inventory.mode==='demo';
  if(!performanceOwner())return renderServingLifecycle()+'<section class=\'panel\'><h2>Performance evidence</h2><p>Owner access is required to inspect sealed performance evidence, create analysis exports, and select an unqualified measured profile.</p></section>';
  if(!state.performanceSelection)state.performanceSelection={evidence_id:demo?'demo-complete':'',evidence_sha256:demo?'0'.repeat(64):'',kind:'comparison'};
  const selected=state.performanceSelection;
  const options=performanceKinds.map(kind=>'<option value=\''+escape(kind)+'\''+(kind===selected.kind?' selected':'')+'>'+escape(kind)+'</option>').join('');
  return renderServingLifecycle()+'<section class=\'panel\'><h2>Offline performance evidence</h2><p>Inspect a sealed owner-approved comparison, coding evaluation, loading, queue, warm-status, cache, or profile-selection bundle. Preview does not run an experiment, change source, restart serving, delete cache data, or qualify hardware.</p>'+(demo?'<p class=\'warning\'>DEMO: all performance evidence is synthetic. It cannot show a GPU speedup, memory reduction, code-execution result, or readiness on this workstation.</p>':'')+'<form id=\'performance-form\'><div class=\'form-grid\'><div><label for=\'performance-kind\'>Analysis kind</label><select id=\'performance-kind\' name=\'kind\'>'+options+'</select></div><div><label for=\'performance-evidence-id\'>Approved sealed evidence ID</label><input id=\'performance-evidence-id\' name=\'evidence_id\' value=\''+escape(selected.evidence_id)+'\' pattern=\'[A-Za-z0-9][A-Za-z0-9-]{7,79}\' required></div><div><label for=\'performance-evidence-sha256\'>Manifest SHA-256</label><input id=\'performance-evidence-sha256\' name=\'evidence_sha256\' value=\''+escape(selected.evidence_sha256)+'\' pattern=\'[a-f0-9]{64}\' maxlength=\'64\' required></div></div><p class=\'muted\'>The helper resolves this ID from root-owned policy and rechecks the exact sealed manifest. Full reports remain private; only bounded summaries are shown here.</p><div class=\'actions\'><button type=\'submit\' data-performance-task=\'preview\'>Inspect sealed evidence</button><button type=\'submit\' data-performance-task=\'plan\'>Review analysis export plan</button><button type=\'submit\' data-performance-task=\'select\'>Review measured-profile selection</button></div></form><div id=\'performance-preview\' aria-live=\'polite\'>'+renderPerformancePreview()+'</div></section><section class=\'panel\'><h2>Retained performance operations</h2>'+operationTable(state.operations.filter(operation=>performanceAction(operation.plan?.draft?.action)))+'</section>';
}
function readPerformanceSelection(form){const data=new FormData(form);return {evidence_id:String(data.get('evidence_id')),evidence_sha256:String(data.get('evidence_sha256')),kind:String(data.get('kind'))};}
function renderPerformancePreview(){return state.performancePreviewError?'<p class=\'error\'>'+escape(state.performancePreviewError)+'</p>':state.performancePreview?performanceSummary(state.performancePreview):'';}
function clearPerformancePreview(){state.performancePreview=null;state.performancePreviewError='';if($('performance-preview'))$('performance-preview').innerHTML='';}
async function performanceSubmit(event){
  event.preventDefault();const form=event.currentTarget,task=event.submitter?.dataset.performanceTask||'preview';state.performanceSelection=readPerformanceSelection(form);const selected={...state.performanceSelection},action=performanceActionFor(selected.kind);
  if(task==='select'&&action!=='performance.profile.select'){notice('Profile selection requires the profile-selection analysis kind.',true);return;}
  if(task==='plan'&&action==='performance.profile.select'){notice('Use the separate measured-profile selection action for profile-selection evidence.',true);return;}
  const draft={action,target:state.inventory.target,source_revision:state.config.revision,performance:selected},controls=[...form.querySelectorAll('button,input,select')];controls.forEach(control=>control.disabled=true);
  try{if(task==='plan'||task==='select'){await submitPlan(action,{performance:selected});return;}clearPerformancePreview();const summary=await api('performance/preview','POST',draft);if(!performanceOwner()||json(state.performanceSelection)!==json(selected))return;state.performancePreview=summary;if($('performance-preview'))$('performance-preview').innerHTML=renderPerformancePreview();notice('Sealed performance evidence inspected. No experiment, source edit, restart, cache deletion, or qualification occurred.');}
  catch(error){state.performancePreviewError=errorMessage(error);if($('performance-preview'))$('performance-preview').innerHTML=renderPerformancePreview();notice(errorMessage(error),true);}
  finally{controls.forEach(control=>control.disabled=false);}
}
function renderProfiles(){return `<section class="panel"><h2>Current profile: ${escape(state.inventory.profile)}</h2><p class="warning">Gaming may unload all AI workloads. Handover requires complete GPU-holder observations and bounded termination. A failed transition can require explicit recovery.</p><p>Sunshine and Steam Remote Play remain the existing alternative session paths. Build inhibition is cooperative; already-running unrelated builds are not suspended.</p></section><div class="cards">${[['ai','AI serving','Restore qualified AI workloads after device ownership is clear.'],['gaming','Gaming session','Stop AI and verify GPU release before the configured gaming path starts.'],['maintenance','Maintenance','Inhibit cooperating builds and stop managed GPU workloads.']].map(([id,title,description])=>`<article class="card"><h2>${title}</h2><p>${description}</p>${action('Preview '+title,'profile.switch',{profile:id})}</article>`).join('')}</div><section class="panel"><h2>Restore previous session</h2><p>Restore uses the durable executor snapshot and rechecks current hardware and device holders.</p>${action('Preview restore','profile.restore')}</section>`;}
function renderBuilds(){const recipes=state.inventory.recipes||[];return `<p class="muted">Only reviewed recipes run in the contained unprivileged worker. Completion creates an artifact; it does not promote, install or qualify it.</p><div class="cards">${recipes.map(r=>`<article class="card"><h2>${escape(r.id)}</h2><span class="pill">${escape(r.status)}</span><p>${escape(r.reason||'Reviewed recipe')}</p><dl><dt>Source revision</dt><dd><code>${escape(r.revision)}</code></dd><dt>Output</dt><dd>${escape(r.output)}</dd><dt>Qualification</dt><dd>${escape(r.qualification)}</dd></dl>${action('Preview queued build','build.start',{recipe:r.id})}</article>`).join('')||'<p>No reviewed recipes are available.</p>'}</div><h2>Build jobs</h2>${operationTable(state.operations.filter(o=>o.plan.draft.action==='build.start'))}`;}
function renderCaches(){const b=state.config.caches;return `<div class="cards">${(state.inventory.caches||[]).map(c=>`<article class="card"><h2>${escape(c.name)}</h2><span class="pill">${escape(c.status)}</span><p>${c.status==='observed'||c.status.startsWith('DEMO')?gib(c.used_bytes):'unknown'} used / ${gib(c.budget_bytes)} budget</p>${detail('Bounded cleanup preview (no deletion)',c.cleanup_preview||[])}</article>`).join('')}</div><section class="panel"><h2>Persistent asset and compilation budgets</h2><form id="caches-form"><div class="form-grid">${field('models_gib','Model cache (GiB)',b.models_gib)}${field('compiler_gib','Compiler cache (GiB)',b.compiler_gib)}${field('shader_gib','Shader cache (GiB)',b.shader_gib)}${field('build_jobs','Compilation jobs',b.build_jobs)}${field('build_memory_mib','Build memory (MiB)',b.build_memory_mib)}${field('scratch_gib','Build scratch (GiB)',b.scratch_gib)}</div><div class="actions"><button type="submit">Preview budget changes</button></div></form><p class="muted">These budgets preserve shared storage and NAS backup ownership. There is no automatic model deletion or general directory cleanup.</p></section>`;}
function renderHarnesses(){return `<p>Manual preference: Qwen Code, then DSH, then Hermes. Export the native bundle and configure the client locally. Credentials stay in supported local secret inputs. RAG access is configured separately.</p><div class="cards">${(state.inventory.harnesses||[]).map(h=>`<article class="card"><h2>${escape(h.id)}</h2><dl><dt>Version</dt><dd><code>${escape(h.version)}</code></dd><dt>Modes</dt><dd>${escape((h.modes||[]).join(', '))}</dd></dl>${(h.limitations||[]).map(l=>`<p class="warning">${escape(l)}</p>`).join('')}<button data-export="${escape(h.id)}">Download native bundle</button><p class="muted"><code>bridgectl harness configure ${escape(h.id)} --directory ./client-profile</code></p></article>`).join('')}</div>`;}
function operationTable(ops) {
  if(!ops.length)return '<div class="panel"><p>No operations yet. Create and review a plan to start.</p></div>';
  return `<div class="table-wrap"><table><thead><tr><th>Operation / action</th><th>Progress</th><th>Durable outcome</th><th>Actions</th></tr></thead><tbody>${ops.map(o=>`<tr>
    <td><code>${escape(o.id)}</code><br>${escape(o.plan.draft.action)}<br><small>${escape(o.updated_at)}</small></td>
    <td><span class="pill">${escape(o.state)}</span><br>${escape(o.phase)}<br>${escape(o.message)}${detail('Progress, artifacts and provenance',o)}</td>
    <td>Source updated: ${o.source_updated?'yes':'no'}<br>Live applied: ${o.live_applied?'yes':'no'}${memoryAction(o.plan.draft.action)?'<p class="muted">Memory evidence/export only. Candidate remains unqualified.</p>':o.recovery_required?'<p class="warning">Recovery required. Inspect host executor state before applying the restore plan.</p>':''}</td>
    <td>${!terminal.has(o.state)?`<button data-cancel="${escape(o.id)}">Request cancellation</button>`:''}
    ${memoryAction(o.plan.draft.action)?memoryOwner()&&o.state!=='succeeded'?`<button data-memory-inspect="${escape(o.id)}">Inspect memory executor</button>`:'':o.recovery_required?`<button data-recover="${escape(o.id)}">Preview recovery</button>`:''}
    ${performanceAction(o.plan.draft.action)&&memoryOwner()&&o.state!=='succeeded'?`<button data-performance-inspect="${escape(o.id)}">Inspect performance executor</button>`:''}
    ${(o.artifacts||[]).map((a,i)=>a.content||(memoryOwner()&&o.plan.draft.action==='memory.plan.export'&&privateMemoryArtifact(a.name))?`<div class="actions"><button data-artifact="${escape(o.id)}" data-index="${i}">Download ${escape(a.name)}</button></div>`:'').join('')}
    ${performanceAction(o.plan.draft.action)?(o.artifacts||[]).map((a,i)=>!a.content?`<div class="actions"><button data-artifact="${escape(o.id)}" data-index="${i}">Download ${escape(a.name)}</button></div>`:'').join(''):''}</td>
    </tr>`).join('')}</tbody></table></div>`;
}
function renderOperations(){return `<p class="muted">Disconnecting or refreshing does not cancel work. Cancellation is a request; wait for a durable terminal outcome. Uncertain external effects stay recovery-required.</p>${operationTable(state.operations)}`;}
function renderPage(){if(!state.inventory)return;state.page=location.hash.slice(1);if(!pages[state.page])state.page='serving';$('page-title').textContent=pages[state.page];for(const a of document.querySelectorAll('[data-page]')){if(a.dataset.page===state.page)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');}const renderers={serving:renderServing,resources:renderResources,performance:renderPerformance,profiles:renderProfiles,builds:renderBuilds,caches:renderCaches,harnesses:renderHarnesses,operations:renderOperations};$('content').innerHTML=renderers[state.page]();bindForms();}
function formNumbers(form){return Object.fromEntries([...new FormData(form)].map(([k,v])=>[k,Number(v)]));}
function bindForms(){
  $('serving-form')?.addEventListener('submit',event=>{event.preventDefault();const f=event.currentTarget,values=formNumbers(f);delete values.model;const gpu=values.gpu_count;delete values.gpu_count;submitPlan('serving.configure',{serving:{...values,model:new FormData(f).get('model')},resources:{...state.config.resources,gpu_count:gpu}});});
  $('resources-form')?.addEventListener('submit',event=>{event.preventDefault();submitPlan('resources.configure',{resources:formNumbers(event.currentTarget)});});
  $('caches-form')?.addEventListener('submit',event=>{event.preventDefault();submitPlan('caches.configure',{caches:formNumbers(event.currentTarget)});});
  $('memory-form')?.addEventListener('submit',memorySubmit);
  $('memory-form')?.addEventListener('input',event=>{state.memorySelection=memorySelection(event.currentTarget);clearMemoryPreview();});
  $('performance-form')?.addEventListener('submit',performanceSubmit);
  $('performance-form')?.addEventListener('input',event=>{state.performanceSelection=readPerformanceSelection(event.currentTarget);clearPerformancePreview();});
}
async function submitPlan(action,extra={}){try{const draft={action,target:state.inventory.target,source_revision:state.config.revision,...extra};const plan=await api('plans','POST',draft);if(performanceAction(action))showPerformancePlan(plan);else showPlan(plan);notice(performanceAction(action)?'Analysis export validated. Review its exact target and bounded evidence metadata before confirming.':'Plan validated. Review consequences and the exact target before applying.');}catch(error){notice(errorMessage(error),true);}}
function showPerformancePlan(plan){
  state.plan=plan;state.applyKey=crypto.randomUUID();const performance=plan.draft.performance||{},preview=plan.preview||{},changes=preview.changes||[];
  const rows=changes.length?'<div class="table-wrap"><table><thead><tr><th>Setting</th><th>Current</th><th>Requested</th></tr></thead><tbody>'+changes.map(change=>'<tr><th>'+escape(change.field)+'</th><td>'+escape(pretty(change.before))+'</td><td>'+escape(pretty(change.after))+'</td></tr>').join('')+'</tbody></table></div>':'<p>No configuration or workload state changes are part of this analysis export.</p>';
  $('plan-summary').innerHTML='<p><strong>'+escape(plan.draft.action)+'</strong> · '+escape(performance.kind||'unknown kind')+' on <code>'+escape(plan.draft.target)+'</code></p><p class="muted">Expires '+escape(plan.expires_at)+'. Sealed evidence <code>'+escape(performance.evidence_id||'unknown')+'</code> is bound to manifest <code>'+escape(performance.evidence_sha256||'unknown')+'</code>.</p>'+rows+'<p class="warning">Confirmation records an unqualified analysis export or selected-profile configuration only. It does not edit source, start or restart serving, modify a Pod, run an experiment, delete cache data, or establish current qualification.</p><p class="muted">Only bounded report metadata is retained with the operation. Private report artifacts require separate owner-only retrieval.</p>'+(preview.consequences||[]).map(value=>'<p class="warning">'+escape(value)+'</p>').join('')+(preview.warnings||[]).map(value=>'<p class="warning">'+escape(value)+'</p>').join('');
  $('plan-json').textContent=json(plan);$('target-confirm').value='';$('target-confirm').placeholder=plan.draft.target;$('apply-consequence').textContent='Confirm the exact target to record this analysis export. It does not apply a workload change or establish a qualified profile.';$('apply-button').textContent='Confirm analysis export';$('plan-dialog').showModal();$('target-confirm').focus();
}
function showPlan(plan){state.plan=plan;state.applyKey=crypto.randomUUID();const preview=plan.preview;const changes=preview.changes||[];const memory=memoryAction(plan.draft.action);$('plan-summary').innerHTML=`<p><strong>${escape(plan.draft.action)}</strong> on <code>${escape(plan.draft.target)}</code></p><p class="muted">Expires ${escape(plan.expires_at)}</p>${changes.length?`<div class="table-wrap"><table><thead><tr><th>Setting</th><th>Current</th><th>Requested</th></tr></thead><tbody>${changes.map(c=>`<tr><th>${escape(c.field)}</th><td>${escape(pretty(c.before))}</td><td>${escape(pretty(c.after))}</td></tr>`).join('')}</tbody></table></div>`:memory?'<p>This plan imports evidence or exports an unqualified memory candidate. It does not change configuration or the running workload.</p>':'<p>This plan changes operational state without editing configuration.</p>'}${(preview.consequences||[]).map(c=>`<p class="warning">${escape(c)}</p>`).join('')}${(preview.warnings||[]).map(c=>`<p class="warning">${escape(c)}</p>`).join('')}`;$('plan-json').textContent=json(plan);$('target-confirm').value='';$('target-confirm').placeholder=plan.draft.target;$('apply-consequence').textContent=memory?'Confirming records the evidence import or candidate export. It does not apply a Pod patch, reduce running memory limits, or qualify the candidate.':'Apply records durable intent and can disrupt workloads. Source updates and live application have separate outcomes.';$('apply-button').textContent=memory?'Confirm evidence / export plan':'Apply approved plan';$('plan-dialog').showModal();$('target-confirm').focus();}
$('login-form').addEventListener('submit',async event=>{event.preventDefault();const button=event.currentTarget.querySelector('button');button.disabled=true;try{const session=await api('auth/login','POST',{credential:$('credential').value});$('credential').value='';showManagement(session);notice('Signed in.');await refresh();}catch(error){$('credential').value='';notice(errorMessage(error),true);}finally{button.disabled=false;}});
$('logout').addEventListener('click',async()=>{try{await api('auth/logout','POST',{});showLogin();notice('Signed out. The browser session is revoked.');}catch(error){notice(errorMessage(error),true);}});
$('refresh').addEventListener('click',()=>refresh().catch(()=>{}));
$('export-source').addEventListener('click',async()=>{try{const config=await api('config/export');const blob=new Blob([json(config)+'\n'],{type:'application/json'}),url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download='managed-source.json';a.click();URL.revokeObjectURL(url);notice('Managed source exported with its content revision. Apply changes through a reviewed plan.');}catch(error){notice(errorMessage(error),true);}});
$('apply-form').addEventListener('submit',async event=>{event.preventDefault();if($('target-confirm').value!==state.plan.draft.target){notice('Type the exact target shown in the plan.',true);return;}const button=$('apply-button');button.disabled=true;try{const operation=await api('operations','POST',{plan_id:state.plan.id,target:$('target-confirm').value},state.applyKey);$('plan-dialog').close();location.hash='operations';notice(`Operation ${operation.id} accepted. Refreshing does not cancel it.`);await refresh();}catch(error){notice(errorMessage(error),true);}finally{button.disabled=false;}});
$('content').addEventListener('click',async event=>{
  const button=event.target.closest('button');
  if(!button)return;
  if(button.form&&button.type==='submit')return;
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
    if(button.dataset.memoryInspect){await api('memory/inspect','POST',{operation_id:button.dataset.memoryInspect});notice('Memory executor inspected. No work was redispatched.');await refresh();}
    if(button.dataset.performanceInspect){await api('performance/inspect','POST',{operation_id:button.dataset.performanceInspect});notice('Performance executor inspected. No work was redispatched.');await refresh();}
    if(button.dataset.export){
      const bundle=await api(`harnesses/${button.dataset.export}/export`);
      download(json(bundle)+'\n',`bridge-${button.dataset.export}-bundle.json`,'application/json');
      notice('Native bundle downloaded. Verify and configure it on the client with bridgectl; launch is client-local.');
    }
    if(button.dataset.artifact){
      const operation=await api(`operations/${button.dataset.artifact}`);let artifact=operation.artifacts[Number(button.dataset.index)];
      if(operation.plan.draft.action==='memory.plan.export'&&privateMemoryArtifact(artifact?.name)){
        const recorded=artifact;artifact=await api('memory/artifact','POST',{operation_id:operation.id,name:recorded.name});
        if(['name','sha256','size','source_revision','qualification'].some(key=>artifact[key]!==recorded[key]))throw new Error('Private memory artifact differs from the recorded operation. Refresh its evidence.');
      }
      if(performanceAction(operation.plan.draft.action)&&!artifact?.content){
        const recorded=artifact;artifact=await api('performance/artifact','POST',{operation_id:operation.id,name:recorded.name});
        if(['name','sha256','size','source_revision','qualification'].some(key=>artifact[key]!==recorded[key]))throw new Error('Private performance artifact differs from the recorded operation. Refresh its evidence.');
      }
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
