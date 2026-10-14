import { useCallback, useEffect, useState } from 'react';
import { ApiError, fetchAgents, fetchSkills, fetchMaterials, saveAgent, saveSkillFile, deleteSkill, type TutorAgent, type TutorAgentInput, type TutorSkill, type Material, type Me } from './api';
import { AppHeader, type Theme } from './AppHeader';
import { useToast } from './Toast';

interface Props { me:Me;theme:Theme;onToggleTheme:()=>void;onLogout:()=>void;onNavigate:(path:string)=>void;onUnauthorized:()=>void; }
const newAgent=():TutorAgentInput=>({name:'',prompt:'',enabled:true,skill_ids:[],material_ids:[]});
const sampleSkill='名称：数学解题引导\n适用条件：方程,去括号,分数,面积,行程\n资料：数学\n工具：knowledge_search\n启用：是\n\n先看懂学生已有的步骤，指出下一步应检查什么。不要给出最终得数或完整解题过程。请学生自己完成关键计算，并标注资料出处。';

export function AssistantSettings({me,theme,onToggleTheme,onLogout,onNavigate,onUnauthorized}:Props){
 const toast=useToast();
 const [agents,setAgents]=useState<TutorAgent[]>([]);
 const [skills,setSkills]=useState<TutorSkill[]>([]);
 const [materials,setMaterials]=useState<Material[]>([]);
 const [tab,setTab]=useState<'agents'|'skills'>('agents');
 const [agentId,setAgentId]=useState<number|undefined>();
 const [input,setInput]=useState<TutorAgentInput>(newAgent);
 const [skillId,setSkillId]=useState<number|undefined>();
 const [source,setSource]=useState(sampleSkill);
 const [busy,setBusy]=useState(false);
 const [loading,setLoading]=useState(true);
 const [message,setMessage]=useState('');
 const [error,setError]=useState('');
 const handleError=useCallback((err:unknown)=>{
  if(err instanceof ApiError&&err.status===401){onUnauthorized();return;}
  let text='无法完成操作，请稍后重试。';
  if(err instanceof ApiError){
   const body=err.body as {message?:string}|null;
   text=body?.message || ({duplicate_name:'本班已有同名配置，请更换名称。',not_found:'所选技能或材料已不存在，请重新加载。',invalid_agent:'名称1–64字、提示词不超过2000字。',invalid_skill:'技能文件格式无效，请检查必填字段和正文。'} as Record<string,string>)[err.code] || (err.status===403?'只有教师可以管理智能体与技能。':text);
  }
  setError(text);toast.push(text,'error');
 },[onUnauthorized,toast]);
 const reload=useCallback(async()=>{
  setLoading(true);
  try{const [a,s,m]=await Promise.all([fetchAgents(),fetchSkills(),fetchMaterials()]);setAgents(a);setSkills(s);setMaterials(m);setError('');}
  catch(err){handleError(err);}finally{setLoading(false);}
 },[handleError]);
 useEffect(()=>{void reload();},[reload]);
 const editAgent=(a:TutorAgent)=>{setAgentId(a.id);setInput({name:a.name,prompt:a.prompt,enabled:a.enabled,skill_ids:a.skill_ids,material_ids:a.material_ids});setError('');setMessage('');};
 const save=async()=>{
  setBusy(true);setError('');setMessage('');
  try{const a=await saveAgent(input,agentId);editAgent(a);await reload();setMessage(`已保存“${a.name}”第${a.version}版，下次提问生效。`);toast.push('智能体已保存','success');}
  catch(err){handleError(err);}finally{setBusy(false);}
 };
 const saveSkill=async()=>{
  setBusy(true);setError('');setMessage('');
  try{const skill=await saveSkillFile(source,skillId);setSkillId(skill.id);setSource(skill.source_text);await reload();setMessage(`技能“${skill.name}”已保存，已绑定的智能体下次提问使用新配置。`);}
  catch(err){handleError(err);}finally{setBusy(false);}
 };
 const remove=async()=>{
  if(!skillId||!window.confirm('删除这个技能及其所有绑定？'))return;
  setBusy(true);try{await deleteSkill(skillId);setSkillId(undefined);setSource(sampleSkill);await reload();setMessage('技能及绑定已删除。');}catch(err){handleError(err);}finally{setBusy(false);}
 };
 const toggleSkill=async(skill:TutorSkill)=>{
  setBusy(true);try{await saveSkillFile(skill.source_text.replace(/^(\s*启用\s*[:：]\s*)(是|否)\s*$/m,`$1${skill.enabled?'否':'是'}`),skill.id);await reload();setMessage('技能开关已更新，下次提问生效。');}catch(err){handleError(err);}finally{setBusy(false);}
 };
 const toggleBound=(type:'skill_ids'|'material_ids',value:number|string)=>{
  setInput(prev=>type==='skill_ids'?{...prev,skill_ids:prev.skill_ids.includes(value as number)?prev.skill_ids.filter(x=>x!==value):[...prev.skill_ids,value as number]}:{...prev,material_ids:prev.material_ids.includes(value as string)?prev.material_ids.filter(x=>x!==value):[...prev.material_ids,value as string]});
 };
 return <div className="app-shell">
  <AppHeader me={me} theme={theme} active="assistant" onToggleTheme={onToggleTheme} onLogout={onLogout} onNavigate={onNavigate}/>
  <main className="config-page">
   <h1>智能体与技能</h1><p>每个智能体独立设置角色、技能和资料范围。会话固定智能体，配置修改从下一轮提问生效。</p>
   <div className="config-tabs" aria-label="配置类型"><button type="button" className={`btn ${tab==='agents'?'btn-primary':''}`} aria-pressed={tab==='agents'} onClick={()=>setTab('agents')}>智能体</button><button type="button" className={`btn ${tab==='skills'?'btn-primary':''}`} aria-pressed={tab==='skills'} onClick={()=>setTab('skills')}>技能文件</button><button type="button" className="btn" onClick={()=>void reload()} disabled={busy}>重新加载</button></div>
   {loading&&<p role="status">正在加载本班配置…</p>}
   {error&&<p role="alert" className="config-error">{error}</p>}
   {message&&<p role="status" className="config-message">{message}</p>}
   {tab==='agents'?<div className="config-grid">
    <section className="config-list" aria-label="本班智能体"><button className="btn btn-primary" type="button" onClick={()=>{setAgentId(undefined);setInput(newAgent());setMessage('');}} disabled={busy}>新建智能体</button>
     {agents.map(a=><button className={`config-item ${a.id===agentId?'selected':''}`} type="button" key={a.id} onClick={()=>editAgent(a)} disabled={busy}><strong>{a.name}</strong><span>{a.enabled?'已启用':'已停用'} · {a.skill_ids.length}个技能 · {a.material_ids.length}份资料</span></button>)}
    </section>
    <form className="config-editor" onSubmit={e=>{e.preventDefault();void save();}}>
     <h2>{agentId?'编辑智能体':'新建智能体'}</h2>
     <label htmlFor="agent-name">名称（本班唯一，1–64字）</label><input id="agent-name" value={input.name} onChange={e=>setInput({...input,name:e.target.value})} disabled={busy}/>
     <label htmlFor="agent-prompt">角色提示词（可留空，最多2000字）</label><textarea id="agent-prompt" rows={7} value={input.prompt} onChange={e=>setInput({...input,prompt:e.target.value})} disabled={busy}/><p className="config-hint">{[...input.prompt].length}/2000；留空时使用默认学习助手角色。</p>
     <label className="config-check"><input type="checkbox" checked={input.enabled} onChange={e=>setInput({...input,enabled:e.target.checked})} disabled={busy}/>启用，允许新会话选择（停用不影响已有会话）</label>
     <fieldset disabled={busy}><legend>绑定技能</legend><p className="config-hint">只在已绑定且启用的技能中按适用条件选择，至多一个。</p>{skills.map(skill=><label className="config-check" key={skill.id}><input type="checkbox" checked={input.skill_ids.includes(skill.id)} onChange={()=>toggleBound('skill_ids',skill.id)}/>{skill.name}（{skill.enabled?'启用':'停用'}）</label>)}{skills.length===0&&<p>先在技能文件页创建技能。</p>}</fieldset>
     <fieldset disabled={busy}><legend>绑定本班资料</legend><p className="config-hint">只检索这些资料；不绑定资料也可回答，届时不显示引用。</p>{materials.map(m=><label className="config-check" key={m.id}><input type="checkbox" checked={input.material_ids.includes(m.id)} onChange={()=>toggleBound('material_ids',m.id)}/>{m.title}（索引{m.index_status}）</label>)}{materials.length===0&&<p>请先到资料库上传文本资料。</p>}</fieldset>
     <button className="btn btn-primary" type="submit" disabled={busy||!input.name.trim()||[...input.name].length>64||[...input.prompt].length>2000}>{busy?'保存中…':'保存智能体'}</button>
    </form>
   </div>:<div className="config-grid">
    <section className="config-list" aria-label="本班技能"><button className="btn btn-primary" type="button" onClick={()=>{setSkillId(undefined);setSource(sampleSkill);setMessage('');}} disabled={busy}>新建技能</button>{skills.map(skill=><div className="config-item" key={skill.id}><button type="button" className="btn" onClick={()=>{setSkillId(skill.id);setSource(skill.source_text);setError('');setMessage('');}} disabled={busy}>{skill.name}</button><span>{skill.trigger_text}</span><button className="btn btn-sm" type="button" disabled={busy} onClick={()=>void toggleSkill(skill)}>{skill.enabled?'停用':'启用'}</button></div>)}</section>
    <form className="config-editor" onSubmit={e=>{e.preventDefault();void saveSkill();}}><h2>{skillId?'编辑技能文件':'新建技能文件'}</h2><label htmlFor="skill-upload">读取UTF-8技能文件（.txt/.md）</label><input id="skill-upload" type="file" accept=".txt,.md" disabled={busy} onChange={async e=>{const file=e.target.files?.[0];if(!file)return;if(file.size>32000){setError('文件过大，最多8000字。');return;}try{setSource(new TextDecoder('utf-8',{fatal:true}).decode(await file.arrayBuffer()));setError('');}catch{setError('技能文件必须使用UTF-8编码。');}}}/>
     <label htmlFor="skill-source">技能文件原文</label><textarea id="skill-source" rows={18} value={source} onChange={e=>setSource(e.target.value)} disabled={busy}/><p className="config-hint">{[...source].length}/8000。名称、适用条件、启用必填；首个空行后为正文。工具只允许 knowledge_search。</p>
     <div className="config-tabs"><button type="submit" className="btn btn-primary" disabled={busy||!source.trim()||[...source].length>8000}>保存技能</button>{skillId&&<button className="btn" type="button" onClick={()=>void remove()} disabled={busy}>删除技能</button>}</div>
    </form>
   </div>}
  </main>
 </div>;
}
