import { expect, it } from "vitest";
import { readChatStream } from "../../../../packages/core/src/ai-stream-result";
function response(text: string) {
  const bytes = new TextEncoder().encode(text);
  return new Response(new ReadableStream({start(controller){for(let i=0;i<bytes.length;i+=3)controller.enqueue(bytes.slice(i,i+3));controller.close();}}));
}
it("跨 UTF-8 与 CRLF 边界收集完整 HTML，保留用量",async()=>{
  const text=['data: '+JSON.stringify({choices:[{delta:{content:'<h1>中文</h1>'}}]}),'data: '+JSON.stringify({choices:[{delta:{},finish_reason:'stop'}],usage:{prompt_tokens:3,completion_tokens:4}}),'data: [DONE]'].join('\r\n\r\n');
  const result=await readChatStream(response(text),'openai');expect(result.content).toBe('<h1>中文</h1>');expect(result.finishReason).toBe('stop');expect(result.usage?.completionTokens).toBe(4);
});
it("断流和上游错误不能作为完整设计，Anthropic 正文不混入思考",async()=>{
  await expect(readChatStream(response('data: {"choices":[{"delta":{"content":"partial"}}]}\n\n'),'openai')).rejects.toThrow('未完整');
  await expect(readChatStream(response('data: {"error":{"message":"secret"}}\n\n'),'openai')).rejects.toThrow('报告错误');
  const blocks=[{type:'content_block_delta',delta:{type:'thinking_delta',thinking:'private'}},{type:'content_block_delta',delta:{type:'text_delta',text:'HTML'}},{type:'message_stop'}];
  expect((await readChatStream(response(blocks.map(b=>'data: '+JSON.stringify(b)).join('\n\n')),'anthropic')).content).toBe('HTML');
});

it("流式进度只计正文字符，保留分块顺序", async () => {
  const received: number[] = [];
  const blocks = [{choices:[{delta:{reasoning_content:"不应计入"}}]}, {choices:[{delta:{content:"正文"}}]}, {choices:[{delta:{content:"继续"}}]}, {choices:[{delta:{},finish_reason:"stop"}]}];
  const result = await readChatStream(response(blocks.map(b => "data: " + JSON.stringify(b)).join("\n\n")), "openai", count => received.push(count));
  expect(received).toEqual([2,4]); expect(result.content).toBe("正文继续");
});

it("收到结束标记后取消仍保持连接的响应，不丢弃结束前的用量", async () => {
  let cancelled = false;
  const body = new ReadableStream({start(controller) { controller.enqueue(new TextEncoder().encode('data: {"choices":[{"delta":{"content":"完成"},"finish_reason":"stop"}]}\n\ndata: {"usage":{"prompt_tokens":3,"completion_tokens":2}}\n\ndata: [DONE]\n\n')); }, cancel() { cancelled = true; }});
  const result = await readChatStream(new Response(body), "openai");
  expect(result.content).toBe("完成"); expect(result.usage?.completionTokens).toBe(2); expect(cancelled).toBe(true);
}, 2000);
