// ferry.js 的测试：用 Node 内置的 test 和 vm 模拟各客户端的脚本环境，不依赖第三方包。
// 运行：node --test script/
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const code = fs.readFileSync(path.join(__dirname, 'ferry.js'), 'utf8');
const cases = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'internal', 'rules', 'testdata', 'extract_cases.json'), 'utf8')).cases;

function load() {
  const holder = {};
  vm.runInNewContext(code, { __FERRY_TEST__: holder, setTimeout, clearTimeout });
  return holder.exports;
}

test('共享样例：匹配与取值和服务端一致', () => {
  const f = load();
  for (const c of cases) {
    const req = { method: c.request.method, url: c.request.url, headers: c.request.headers || {}, body: c.request.body };
    if (c.matcher) {
      const m = { host: c.matcher.host, path: c.matcher.path_regex, method: c.matcher.method };
      assert.equal(f.matchRequest(m, req), c.match, c.name);
    }
    if (c.field) {
      const v = f.extractField({ in: c.field.in, path: c.field.path, regex_on: c.field.regex_on }, req);
      assert.equal(v !== undefined, c.ok, c.name);
      if (c.ok) assert.equal(v, c.value, c.name);
    }
  }
});

// 和服务端 BuildScriptRules 对九号示例的输出同形。
const RULES = {
  sources: [
    {
      key: 'ninebot',
      name: '九号出行',
      notify: 'always',
      min_interval: 60,
      matchers: [
        { name: 'gateway', host: 'cn-cbu-gateway.ninebot.com', path: '^/portal/api/' },
        { name: 'sign', host: 'cn-cbu-gateway.ninebot.com', path: '^/portal/api/user-sign/v2/sign$', method: 'POST', body: true },
      ],
      fields: [
        { key: 'authorization', matcher: 'gateway', in: 'header', path: 'Authorization' },
        { key: 'device_id', matcher: 'sign', in: 'body_json', path: 'deviceId' },
      ],
    },
  ],
};

const BASE = 'http://192.168.1.10:8080';
const SIGN = {
  method: 'POST',
  url: 'https://cn-cbu-gateway.ninebot.com/portal/api/user-sign/v2/sign',
  headers: { authorization: 'eyJhbGciOiJIUzI1NiJ9.e30.sig', 'Content-Type': 'application/json' },
  body: '{"deviceId":"DEV-001"}',
};

// fakeServer 模拟平台：记录收到的请求，按路径返回。
function fakeServer(opts = {}) {
  const calls = [];
  function handle(req) {
    calls.push(req);
    if (opts.down && req.url.startsWith(BASE)) return { error: 'connection refused' };
    if (opts.hang) return { hang: true };
    if (req.headers['X-Ferry-Subscription'] !== 'fs_tok') return { status: 401, headers: {}, body: '{}' };
    if (req.url.endsWith('/api/v1/rules')) {
      if (req.headers['If-None-Match'] === '"e1"') return { status: 304, headers: { ETag: '"e1"' }, body: '' };
      return { status: 200, headers: { ETag: '"e1"' }, body: JSON.stringify(RULES) };
    }
    if (req.url.endsWith('/api/v1/sync')) return { status: 200, headers: {}, body: '{"result":"accepted","message":"已保存新版本"}' };
    return { status: 404, headers: {}, body: '' };
  }
  return { calls, handle, syncs: () => calls.filter((c) => c.url.endsWith('/api/v1/sync')) };
}

// makeClient 按客户端构造脚本能看到的全局变量。store 跨多次运行保留，模拟手机上的持久化存储。
function makeClient(kind, server, store, request, args) {
  const notes = [];
  const httpCalls = [];
  const argString = args || `base=${encodeURIComponent(BASE)}&token=fs_tok`;
  const ctx = { setTimeout, clearTimeout, $request: request };
  function respond(r, cb) {
    if (r.hang) return;
    setImmediate(() => (r.error ? cb(r.error) : cb(null, { status: r.status, headers: r.headers }, r.body)));
  }
  if (kind === 'qx') {
    ctx.$task = {
      fetch(req) {
        httpCalls.push(req);
        const r = server.handle({ url: req.url, method: req.method, headers: req.headers || {}, body: req.body });
        return new Promise((resolve, reject) => {
          if (r.hang) return;
          setImmediate(() => (r.error ? reject({ error: r.error }) : resolve({ statusCode: r.status, headers: r.headers, body: r.body })));
        });
      },
    };
    ctx.$prefs = { valueForKey: (k) => store.get(k) ?? null, setValueForKey: (v, k) => (store.set(k, v), true) };
    ctx.$notify = (t, s, b) => notes.push([t, s, b]);
    ctx.$environment = { sourcePath: `${BASE}/script/ferry.js#${argString}` };
  } else {
    const client = {};
    for (const m of ['get', 'post']) {
      client[m] = (opts, cb) => {
        httpCalls.push({ ...opts, method: m.toUpperCase() });
        respond(server.handle({ url: opts.url, method: m.toUpperCase(), headers: opts.headers || {}, body: opts.body }), cb);
      };
    }
    ctx.$httpClient = client;
    ctx.$persistentStore = { read: (k) => store.get(k) ?? null, write: (v, k) => (store.set(k, v), true) };
    ctx.$notification = { post: (t, s, b) => notes.push([t, s, b]) };
    ctx.$argument = argString;
    if (kind === 'loon') ctx.$loon = 'Loon';
    if (kind === 'surge') ctx.$environment = { 'surge-version': '5.0' };
    if (kind === 'stash') ctx.$environment = { 'stash-version': '3.0' };
  }
  return { ctx, notes, httpCalls };
}

// assertDoneOnce 断言 $done 恰好被调用一次，参数是空对象。沙箱里的对象原型不同，按 JSON 比较。
function assertDoneOnce(dones) {
  assert.equal(JSON.stringify(dones), '[{}]');
}

// runScript 执行一次脚本，等到 $done 被调用；返回调用参数和次数。
function runScript(c) {
  return new Promise((resolve, reject) => {
    const dones = [];
    c.ctx.$done = (v) => {
      dones.push(v);
      setTimeout(() => resolve(dones), 50);
    };
    const timer = setTimeout(() => reject(new Error('脚本没有调用 $done')), 12000);
    timer.unref();
    vm.runInNewContext(code, c.ctx);
  });
}

for (const kind of ['surge', 'loon', 'qx', 'stash', 'egern']) {
  test(`${kind}：取到字段后回传一次，值不变时不再回传`, async () => {
    const server = fakeServer();
    const store = new Map();
    const c1 = makeClient(kind, server, store, { ...SIGN });
    assertDoneOnce(await runScript(c1));
    const syncs = server.syncs();
    assert.equal(syncs.length, 1);
    const body = JSON.parse(syncs[0].body);
    assert.equal(body.source, 'ninebot');
    assert.deepEqual(body.fields, { authorization: 'eyJhbGciOiJIUzI1NiJ9.e30.sig', device_id: 'DEV-001' });
    assert.equal(c1.notes.length, 1);
    assert.equal(c1.notes[0][0], 'Ferry · 九号出行');
    assert.ok(!c1.notes[0][2].includes('DEV-001'), '通知里不能有字段值');
    if (kind === 'loon') assert.equal(c1.httpCalls[0].timeout, 3000, 'Loon 的超时单位是毫秒');
    if (kind === 'surge') assert.equal(c1.httpCalls[0].timeout, 3);

    const c2 = makeClient(kind, server, store, { ...SIGN });
    assertDoneOnce(await runScript(c2));
    assert.equal(server.syncs().length, 1, '值没变不应再回传');
    assert.equal(server.calls.filter((x) => x.url.endsWith('/rules')).length, 1, '缓存未过期时不应再拉规则');
  });
}

test('不匹配的请求直接放行', async () => {
  const server = fakeServer();
  const c = makeClient('surge', server, new Map(), { method: 'GET', url: 'https://www.apple.com/', headers: {} });
  assertDoneOnce(await runScript(c));
  assert.equal(server.syncs().length, 0);
});

test('平台连不上、也没有规则缓存时直接放行', async () => {
  const server = fakeServer({ down: true });
  const c = makeClient('surge', server, new Map(), { ...SIGN });
  assertDoneOnce(await runScript(c));
  assert.equal(server.syncs().length, 0);
  assert.equal(c.notes.length, 0, '不知道是哪个来源，不发通知');
});

test('有规则缓存但回传失败时放行，并在手机上提示失败', async () => {
  const store = new Map([['ferry.rules', JSON.stringify({ etag: '"e1"', fetchedAt: Date.now(), rules: RULES })]]);
  const c = makeClient('surge', fakeServer({ down: true }), store, { ...SIGN });
  assertDoneOnce(await runScript(c));
  assert.equal(c.notes.length, 1);
  assert.equal(c.notes[0][1], '回传失败');
  assert.ok(!store.has('ferry.last.ninebot'), '失败时不记录，下次还会重试');
});

test('主地址连不上时改用备用地址', async () => {
  const server = fakeServer({ down: true });
  const args = `base=${encodeURIComponent(BASE)}&fallback=${encodeURIComponent('https://ferry.example.com')}&token=fs_tok`;
  const c = makeClient('surge', server, new Map(), { ...SIGN }, args);
  assertDoneOnce(await runScript(c));
  const syncs = server.syncs().filter((x) => x.url.startsWith('https://ferry.example.com'));
  assert.equal(syncs.length, 1);
});

test('平台无响应时超时放行', async () => {
  const server = fakeServer({ hang: true });
  const c = makeClient('surge', server, new Map(), { ...SIGN });
  const started = Date.now();
  assertDoneOnce(await runScript(c));
  assert.ok(Date.now() - started < 9000, '应在超时后放行');
});

test('缺少参数时直接放行', async () => {
  const server = fakeServer();
  const c = makeClient('surge', server, new Map(), { ...SIGN }, 'token=fs_tok');
  assertDoneOnce(await runScript(c));
  assert.equal(server.calls.length, 0);
});

test('脚本内部出错也放行', async () => {
  const server = fakeServer();
  const req = { ...SIGN };
  Object.defineProperty(req, 'headers', { get() { throw new Error('boom'); } });
  const c = makeClient('surge', server, new Map(), req);
  assertDoneOnce(await runScript(c));
});

test('规则缓存过期后带 ETag 重新拉取，304 时沿用缓存', async () => {
  const server = fakeServer();
  const store = new Map();
  await runScript(makeClient('surge', server, store, { method: 'GET', url: 'https://www.apple.com/', headers: {} }));
  const cached = JSON.parse(store.get('ferry.rules'));
  cached.fetchedAt = 0;
  store.set('ferry.rules', JSON.stringify(cached));
  const c = makeClient('surge', server, store, { ...SIGN });
  assertDoneOnce(await runScript(c));
  const ruleCalls = server.calls.filter((x) => x.url.endsWith('/rules'));
  assert.equal(ruleCalls.length, 2);
  assert.equal(ruleCalls[1].headers['If-None-Match'], '"e1"');
  assert.equal(server.syncs().length, 1, '304 时用缓存的规则照样能回传');
});

const transportCases = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'internal', 'transport', 'testdata', 'sealed_cases.json'), 'utf8'));
const hex = (b) => Buffer.from(b).toString('hex');

test('传输加密共享样例：子密钥与服务端一致', () => {
  const f = load();
  for (const c of transportCases.keys) {
    const k = f.transportKeys(c.key);
    assert.equal(hex(k.up), c.up, c.key);
    assert.equal(hex(k.down), c.down, c.key);
  }
  for (const c of transportCases.invalid_keys) assert.equal(f.transportKeys(c.key), null, c.name);
});

test('传输加密共享样例：加密结果与服务端逐字节一致，且能解开', () => {
  const f = load();
  for (const c of transportCases.sealed) {
    const k = f.transportKeys(c.key)[c.dir];
    const nonce = new Uint8Array(Buffer.from(c.nonce, 'hex'));
    assert.equal(f.sealMessage(k, nonce, c.plaintext), c.sealed, c.name);
    const opened = f.openMessage(k, c.sealed);
    assert.ok(opened, c.name);
    assert.equal(opened.plaintext, c.plaintext, c.name);
    assert.equal(hex(opened.nonce), c.nonce, c.name);
  }
});

test('传输加密共享样例：篡改、错钥、错方向、格式不对都解不开', () => {
  const f = load();
  for (const c of transportCases.invalid) {
    assert.equal(f.openMessage(f.transportKeys(c.key)[c.dir], c.sealed), null, c.name);
  }
});

test('UTF-8 编解码与 Node 一致，不合法的字节拒绝', () => {
  const f = load();
  for (const s of ['', 'ascii', '中文', 'é', '✅', '𝄞🙂', '\u0000\u007f\u0080\u07ff\u0800\ufffd', '落单\ud800代理']) {
    assert.equal(hex(f.utf8Encode(s)), hex(Buffer.from(s, 'utf8')), JSON.stringify(s));
    assert.equal(f.utf8Decode(f.utf8Encode(s)), Buffer.from(s, 'utf8').toString('utf8'), JSON.stringify(s));
  }
  for (const b of [[0x80], [0xc0, 0x80], [0xe0, 0x80, 0x80], [0xed, 0xa0, 0x80], [0xf4, 0x90, 0x80, 0x80], [0xe4, 0xb8], [0xff]]) {
    assert.equal(f.utf8Decode(new Uint8Array(b)), null, hex(b));
  }
});

test('base64url 解码：与 Node 一致，非规范写法拒绝', () => {
  const f = load();
  for (let n = 0; n < 40; n++) {
    const b = Buffer.alloc(n, 0).map((_, i) => (i * 37 + n) & 255);
    assert.equal(hex(f.b64urlDecode(b.toString('base64url'))), hex(b), String(n));
  }
  // 依次是：长度不对、带填充、标准 base64 字符、末尾多余位不为零（两种长度）。
  for (const s of ['A', 'AB=', 'AB+/', 'AB', 'AAB']) assert.equal(f.b64urlDecode(s), null, s);
});

// ---------- 加密通道 ----------

const KID = 'abcdefghijklmnop';
const KEY = Buffer.alloc(32, 7).toString('base64url');

// sealedServer 模拟平台的加密入口：用同一套加解密函数解开请求、检查重放、按 op 回复。
// opts.mode 可以是 'gone'（订阅已切回明文）、'clock'（时间超窗）、'garbage'（回复解不开）、'wrongRe'（回复对应别的请求）。
function sealedServer(opts = {}) {
  const f = load();
  const keys = f.transportKeys(opts.key || KEY);
  const calls = [];
  const opened = [];
  const seen = new Set();
  function handle(req) {
    calls.push(req);
    if (!req.url.endsWith('/api/v1/sealed')) return { status: 404, headers: {}, body: '' };
    if (req.headers['X-Ferry-Kid'] !== KID) return { status: 401, headers: {}, body: '{}' };
    const o = f.openMessage(keys.up, req.body);
    if (!o) return { status: 401, headers: {}, body: '{}' };
    const msg = JSON.parse(o.plaintext);
    opened.push({ msg, length: Buffer.byteLength(o.plaintext) });
    const re = f.b64urlEncode(o.nonce);
    let rep;
    if (seen.has(re)) rep = { status: 409 };
    else if (opts.mode === 'gone') rep = { status: 410, body: { error: 'gone' } };
    else if (opts.mode === 'clock') rep = { status: 409, time: Date.now() };
    else if (msg.op === 'ping') rep = { status: 200, body: { ok: true } };
    else if (msg.op === 'rules') rep = msg.etag === '"e1"' ? { status: 304, etag: '"e1"' } : { status: 200, etag: '"e1"', body: RULES };
    else if (msg.op === 'sync') rep = { status: 200, body: { result: 'accepted', message: '收到 2 个新值' } };
    else rep = { status: 400 };
    seen.add(re);
    rep.v = 1;
    rep.re = opts.mode === 'wrongRe' ? 'AAAA' : re;
    if (opts.mode === 'garbage') return { status: 200, headers: {}, body: 'f1.AAAA' };
    const nonce = new Uint8Array(24).map(() => Math.floor(Math.random() * 256));
    return { status: 200, headers: {}, body: f.sealMessage(keys.down, nonce, JSON.stringify(rep)) };
  }
  return { calls, opened, handle, ops: () => opened.map((x) => x.msg.op) };
}

function sealedArgs(key) {
  return `base=${encodeURIComponent(BASE)}&kid=${KID}&mode=sealed` + (key === undefined ? `&key=${KEY}` : `&key=${key}`);
}

for (const kind of ['surge', 'loon', 'stash', 'egern']) {
  test(`${kind}：加密通道先 ping，再拉规则、回传；报文里没有明文`, async () => {
    const server = sealedServer();
    const store = new Map();
    const c1 = makeClient(kind, server, store, { ...SIGN }, sealedArgs());
    assertDoneOnce(await runScript(c1));
    assert.deepEqual(server.ops(), ['ping', 'rules', 'sync']);
    for (const call of server.calls) {
      assert.ok(call.body.startsWith('f1.'), '请求正文应是密文');
      assert.ok(!call.body.includes('DEV-001') && !call.body.includes('ninebot'), '密文里不能有明文');
      assert.equal(call.headers['X-Ferry-Subscription'], undefined, '加密通道不带订阅令牌');
    }
    for (const o of server.opened) assert.equal(o.length % 256, 0, '请求明文应补齐到 256 的整数倍');
    assert.equal(server.opened[2].msg.body.fields.device_id, 'DEV-001');
    assert.deepEqual(c1.notes.map((n) => n[1]), ['传输已加密', '收到 2 个新值']);
    assert.equal(store.get('ferry.sealed'), '1');

    const c2 = makeClient(kind, server, store, { ...SIGN }, sealedArgs());
    assertDoneOnce(await runScript(c2));
    assert.deepEqual(server.ops(), ['ping', 'rules', 'sync'], '密钥没变不再 ping，值没变不再回传');
  });
}

test('加密通道：规则缓存过期后带 ETag 拉取，304 时沿用缓存', async () => {
  const server = sealedServer();
  const store = new Map();
  await runScript(makeClient('surge', server, store, { method: 'GET', url: 'https://www.apple.com/', headers: {} }, sealedArgs()));
  const cached = JSON.parse(store.get('ferry.rules'));
  cached.fetchedAt = 0;
  store.set('ferry.rules', JSON.stringify(cached));
  assertDoneOnce(await runScript(makeClient('surge', server, store, { ...SIGN }, sealedArgs())));
  assert.deepEqual(server.ops(), ['ping', 'rules', 'rules', 'sync']);
  assert.equal(server.opened[2].msg.etag, '"e1"');
});

test('加密通道：密钥填错时提示一次，不重复提示回传失败', async () => {
  const server = sealedServer({ key: Buffer.alloc(32, 9).toString('base64url') });
  const store = new Map([['ferry.rules', JSON.stringify({ etag: '"e1"', fetchedAt: Date.now(), rules: RULES })]]);
  const c1 = makeClient('surge', server, store, { ...SIGN }, sealedArgs());
  assertDoneOnce(await runScript(c1));
  assert.deepEqual(c1.notes.map((n) => n[1]), ['传输密钥错误']);
  assert.ok(!store.has('ferry.last.ninebot'));
  const c2 = makeClient('surge', server, store, { ...SIGN }, sealedArgs());
  assertDoneOnce(await runScript(c2));
  assert.equal(c2.notes.length, 0, '一小时内不重复提示');
});

test('加密通道：没填密钥（含未替换的占位符）时不联网，只提示一次', async () => {
  for (const key of ['', '{{{key}}}', '${args.key}', '{key}']) {
    const server = sealedServer();
    const store = new Map();
    const c = makeClient('surge', server, store, { ...SIGN }, sealedArgs(key));
    assertDoneOnce(await runScript(c));
    assert.equal(server.calls.length, 0, key);
    assert.deepEqual(c.notes.map((n) => n[1]), ['尚未填写传输密钥'], key);
  }
});

test('加密通道：走过加密后，参数被改成明文也不发明文', async () => {
  const server = sealedServer();
  const store = new Map();
  await runScript(makeClient('surge', server, store, { ...SIGN }, sealedArgs()));
  const plain = fakeServer();
  const args = `base=${encodeURIComponent(BASE)}&token=fs_tok&kid=${KID}`;
  const c = makeClient('surge', plain, store, { ...SIGN, headers: { ...SIGN.headers, authorization: 'changed' } }, args);
  assertDoneOnce(await runScript(c));
  assert.equal(plain.calls.length, 0, '不能退回明文');
  assert.deepEqual(c.notes.map((n) => n[1]), ['尚未填写传输密钥']);
});

test('加密通道：回复解不开或对应别的请求时按失败处理', async () => {
  for (const mode of ['garbage', 'wrongRe']) {
    const store = new Map([
      ['ferry.rules', JSON.stringify({ etag: '"e1"', fetchedAt: Date.now(), rules: RULES })],
      ['ferry.keyfp', 'x'],
    ]);
    const c = makeClient('surge', sealedServer({ mode }), store, { ...SIGN }, sealedArgs());
    assertDoneOnce(await runScript(c));
    assert.deepEqual(c.notes.map((n) => n[1]), ['加密连接失败', '回传失败'], mode);
    assert.ok(c.notes[1][2].startsWith('响应校验失败'), mode);
    assert.ok(!store.has('ferry.last.ninebot'), mode);
  }
});

test('加密通道：订阅切回明文（410）时清掉本地记录并提示重装', async () => {
  const store = new Map([['ferry.sealed', '1'], ['ferry.keyfp', 'x']]);
  const c = makeClient('surge', sealedServer({ mode: 'gone' }), store, { ...SIGN }, sealedArgs());
  assertDoneOnce(await runScript(c));
  assert.ok(c.notes.some((n) => n[1] === '订阅已切换为明文传输'));
  assert.equal(store.get('ferry.sealed'), '', '本地“走过加密”的记录应清掉');
  // 重新安装成明文模块后（参数里有令牌、没有密钥），同一台手机能正常走明文。
  const plain = fakeServer();
  assertDoneOnce(await runScript(makeClient('surge', plain, store, { ...SIGN })));
  assert.equal(plain.syncs().length, 1);
});

test('加密通道：时间超窗时提示校时', async () => {
  const store = new Map([['ferry.rules', JSON.stringify({ etag: '"e1"', fetchedAt: Date.now(), rules: RULES })]]);
  const c = makeClient('surge', sealedServer({ mode: 'clock' }), store, { ...SIGN }, sealedArgs());
  assertDoneOnce(await runScript(c));
  assert.deepEqual(c.notes.map((n) => n[1]), ['设备时间不准确']);
});

test('nonce：有安全随机数时用它，没有时拼出来也不重复', () => {
  const holder = {};
  vm.runInNewContext(code, { __FERRY_TEST__: holder, setTimeout, clearTimeout });
  const f = holder.exports;
  const store = new Map();
  const env = { read: (k) => store.get(k) ?? null, write: (v, k) => store.set(k, v) };
  const seen = new Set();
  for (let i = 0; i < 1000; i++) seen.add(Buffer.from(f.makeNonce({}, env)).toString('hex'));
  assert.equal(seen.size, 1000);
  assert.equal(store.get('ferry.nonce'), '1000');
  let used = 0;
  const g = { crypto: { getRandomValues: (a) => (used++, a.fill(5)) } };
  assert.equal(Buffer.from(f.makeNonce(g, env)).toString('hex'), '05'.repeat(24));
  assert.equal(used, 1);
});

test('loon：$argument 是对象（新参数语法）时照样走加密通道', async () => {
  const server = sealedServer();
  const c = makeClient('loon', server, new Map(), { ...SIGN });
  c.ctx.$argument = { base: BASE, fallback: '', kid: KID, mode: 'sealed', key: KEY };
  assertDoneOnce(await runScript(c));
  assert.deepEqual(server.ops(), ['ping', 'rules', 'sync']);
});
