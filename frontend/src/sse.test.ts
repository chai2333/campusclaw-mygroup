import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer, type ServerResponse } from 'node:http';
import { StreamError, streamAssistantAnswer, type StreamEvent } from './sse.ts';

const citation={ordinal:1,material_id:'mat-1',material_title:'数学',chunk_id:1,chunk_index:0,char_start:0,char_end:5,excerpt:'摘录'};
const meta={status:'ok',conversation_id:'c1',user_message_id:'u1',assistant_message_id:'a1',skill:{id:1,name:'数学解题引导'},citations:[citation]};
const done={assistant_message_id:'a1',status:'completed',citations:[citation]};
const frame=(event:string,data:unknown)=>`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
async function harness(body:(res:ServerResponse)=>void){
 const requests:Array<{text:string;auth:string}> = [];
 const server=createServer((req,res)=>{let raw='';req.on('data',x=>raw+=x.toString());req.on('end',()=>{requests.push({text:JSON.parse(raw).text,auth:req.headers.authorization??''});body(res);});});
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));
 const address=server.address();assert.ok(address&&typeof address==='object');
 return {url:`http://127.0.0.1:${address.port}`,requests,close:()=>new Promise<void>(resolve=>{server.close(()=>resolve());server.closeAllConnections();})};
}
async function run(raw:string,fragment=0){
 const h=await harness(res=>{res.writeHead(200,{'Content-Type':'text/event-stream'});const bytes=Buffer.from(raw);if(fragment){for(let i=0;i<bytes.length;i+=fragment)res.write(bytes.subarray(i,i+fragment));res.end();}else res.end(bytes);});
 const events:StreamEvent[]=[];
 try{return {terminal:await streamAssistantAnswer({baseUrl:h.url,conversationId:'c1',question:'测试问题',token:'token-test',onEvent:e=>events.push(e)}),events,requests:h.requests};}finally{await h.close();}
}

test('meta exposes skill and citations before arbitrary fragmented UTF8 deltas',async()=>{
 const result=await run(frame('meta',meta)+frame('delta',{text:'先检查\n分母。'})+frame('delta',{text:'请你继续。'})+frame('done',done),1);
 assert.equal(result.terminal.status,'completed');assert.deepEqual(result.events.map(x=>x.type),['meta','delta','delta']);
 assert.equal(result.events[0].type,'meta');if(result.events[0].type==='meta'){assert.equal(result.events[0].skill?.name,'数学解题引导');assert.deepEqual(result.events[0].citations,[citation]);}
 assert.deepEqual(result.requests,[{text:'测试问题',auth:'Bearer token-test'}]);
});
test('null skill and empty citations are valid',async()=>{
 const result=await run(frame('meta',{...meta,skill:null,citations:[]})+frame('delta',{text:'继续作答'})+frame('done',{...done,citations:[]}));assert.equal(result.terminal.status,'completed');
});
test('explicit retryability and safe error code survive',async()=>{
 const result=await run(frame('meta',meta)+frame('error',{code:'model_timeout',message:'超时',retryable:true}));assert.deepEqual(result.terminal,{status:'error',code:'model_timeout',message:'超时',retryable:true});
});
test('missing terminal means unfinished connection',async()=>{const r=await run(frame('meta',meta)+frame('delta',{text:'一部分'}));assert.equal(r.terminal.status,'error');if(r.terminal.status==='error')assert.equal(r.terminal.code,'stream_closed');});
for(const [name,raw] of [
 ['delta before meta',frame('delta',{text:'错误'})],
 ['duplicate meta',frame('meta',meta)+frame('meta',meta)],
 ['event after done',frame('meta',meta)+frame('done',done)+frame('delta',{text:'错误'})],
 ['old start protocol',frame('start',meta)],
 ['malformed JSON','event: meta\ndata: {\n\n'],
 ['mismatched done message',frame('meta',meta)+frame('done',{...done,assistant_message_id:'other'})],
 ['mismatched done citations',frame('meta',meta)+frame('done',{...done,citations:[]})],
 ['nonconsecutive citation ordinals',frame('meta',{...meta,citations:[{...citation,ordinal:2}]})],
 ['multiline JSON data','event: meta\ndata: {}\ndata: {}\n\n'],
] as const){test(name,async()=>{await assert.rejects(()=>run(raw),err=>err instanceof StreamError&&err.code==='protocol_error');});}
test('pre-stream JSON errors preserve 503 and code',async()=>{
 const h=await harness(res=>{res.writeHead(503,{'Content-Type':'application/json'});res.end('{"error":"retrieval_unavailable"}');});
 try{await assert.rejects(()=>streamAssistantAnswer({baseUrl:h.url,conversationId:'c1',question:'问题',token:'t',onEvent:()=>{}}),err=>err instanceof StreamError&&err.status===503&&err.code==='retrieval_unavailable'&&err.retryable);}finally{await h.close();}
});
test('stop aborts stream after meta',async()=>{
 const h=await harness(res=>{res.writeHead(200,{'Content-Type':'text/event-stream'});res.write(frame('meta',meta));});
 const controller=new AbortController();
 try{await assert.rejects(()=>streamAssistantAnswer({baseUrl:h.url,conversationId:'c1',question:'问题',token:'t',signal:controller.signal,onEvent:e=>{if(e.type==='meta')controller.abort();}}),err=>err instanceof DOMException&&err.name==='AbortError');}finally{await h.close();}
});
