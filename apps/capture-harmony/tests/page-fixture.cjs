// 页面级测试的公共夹具：真实 DraftQueue、真实 SentLog（内存端口）与可编排的中转客户端。
// 只替换系统边界；发送、记录、分页等控制逻辑都走页面真实代码。
const assert = require('node:assert/strict');
const { load, settle, createPage, check, sentLog, memorySentPort } = require('./support.cjs');

const protocol = load('core/Protocol.ts');
const outcome = load('core/SendOutcome.ts', { './ConnectionCheck': check });
const { DraftQueue } = load('core/DraftQueue.ts', { './Protocol': protocol, './SendOutcome': outcome });

const receipt = requestId => ({ id: `receipt-${requestId}`, requestId, createdAt: 1, itemCount: 1, state: 'accepted', progress: null });
let uuidCounter = 0;
const uuid = () => `00000000-0000-4000-8000-${String(++uuidCounter).padStart(12, '0')}`;

// clientParts 覆盖默认的 session / history / send；inputs 是预先保存为草稿的输入；extraImports 补充页面依赖的系统模块桩。
async function pageWith(clientParts = {}, inputs = ['https://example.com/a'], extraImports = {}) {
  const stats = { sessions: 0, histories: 0, sends: [] };
  const sendImpl = clientParts.send ?? (async body => receipt(body.requestId));
  const client = {
    session: async () => { stats.sessions++; return { paired: true }; },
    history: async () => { stats.histories++; return []; },
    ...clientParts,
    // 统一在最外层计数，自定义 send 也被记录。
    send: body => { stats.sends.push(body.requestId); return sendImpl(body); },
  };
  const copied = [];
  const { ui, time, haptics } = createPage(client, {
    '@kit.ArkTS': { util: { generateRandomUUID: () => uuid() } },
    '@kit.BasicServicesKit': { pasteboard: {
      MIMETYPE_TEXT_PLAIN: 'text/plain', createData: (mime, text) => ({ mime, text }),
      getSystemPasteboard: () => ({ setData: async data => { copied.push(data.text); } }) } },
    ...extraImports,
  });
  const store = { state: null, async save(state) { this.state = JSON.parse(JSON.stringify(state)); } };
  const queue = new DraftQueue(store, new protocol.LocalState());
  for (const input of inputs) { await queue.edit(input, 'auto'); await queue.enqueue(uuid()); }
  // 发送记录使用真实的 SentLog 与可编排失败的内存端口，与真实运行时同型。
  const sentPort = memorySentPort();
  ui.runtime = { client, connection: { origin: 'https://example.invalid', pairingId: 'computer-a' }, queue, sentLog: new sentLog.SentLog(sentPort) };
  ui.drafts = queue.snapshot().drafts; ui.paired = true;
  ui.selectTab = () => {}; ui.syncHolding = () => {}; ui.startPolling = () => {};
  ui.settings = { autoSendShare: false, defaultMode: 'auto' }; ui.settingsReady = true;
  return { ui, time, queue, store, stats, copied, client, sentPort, haptics };
}

// 等待后台 Promise 链（串行步骤有多次 await）走完。
async function until(condition, limit = 200) { for (let i = 0; i < limit && !condition(); i++) await settle(); assert.ok(condition(), '等待条件未在预期内满足'); }

module.exports = { protocol, outcome, DraftQueue, receipt, uuid, pageWith, until };
