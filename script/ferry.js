/*
 * Ferry 通用脚本：在本人手机的代理客户端里运行，把本人账号的登录态同步到自己的 Ferry 平台。
 *
 * - 规则（哪些请求、取哪些字段）运行时从平台拉取，带 ETag 和本地缓存；只取规则里声明过的字段。
 * - 值没变就不回传；回传结果按来源设置在手机上通知（默认每次都通知）。
 * - 订阅开了加密时，整个请求用模块参数里的传输密钥加密（NaCl secretbox），访问地址是 http 也读不到、改不了；
 *   格式和服务端 internal/transport 一致。走过加密后不再退回明文。
 * - 无论成功、失败还是脚本出错，最后都以 $done({}) 放行原请求。
 *   Loon、Stash 里不带参数的 $done() 会断开请求，所以一律传空对象。
 *
 * 取值语义和服务端 internal/rules 一致，两边共用 internal/rules/testdata/extract_cases.json 测试。
 * 只用 ES2015 语法，兼容各客户端较旧的 JS 引擎。
 */

var RULES_KEY = 'ferry.rules';
var LAST_KEY_PREFIX = 'ferry.last.';
var RULES_TTL_MS = 10 * 60 * 1000;
var HTTP_TIMEOUT_MS = 3000;
var SAFETY_TIMEOUT_MS = 8000;
// SEALED_KEY 记着这台手机走过加密通道；KEY_FP_KEY 是上次连通时“编号 + 密钥”的指纹。
var SEALED_KEY = 'ferry.sealed';
var KEY_FP_KEY = 'ferry.keyfp';
var NONCE_COUNTER_KEY = 'ferry.nonce';
var WARN_KEY_PREFIX = 'ferry.warn.';
var WARN_INTERVAL_MS = 60 * 60 * 1000;

// ---------- 环境适配 ----------

function makeEnv(g) {
  var isQX = typeof g.$task !== 'undefined';
  var isLoon = typeof g.$loon !== 'undefined';

  function read(key) {
    try {
      return isQX ? g.$prefs.valueForKey(key) : g.$persistentStore.read(key);
    } catch (e) {
      return null;
    }
  }
  // write 的参数顺序和 $persistentStore.write 一样：先值后键。
  function write(value, key) {
    try {
      return isQX ? g.$prefs.setValueForKey(value, key) : g.$persistentStore.write(value, key);
    } catch (e) {
      return false;
    }
  }
  function notify(title, subtitle, body) {
    try {
      if (isQX) g.$notify(title, subtitle, body);
      else g.$notification.post(title, subtitle, body);
    } catch (e) {
      // 通知失败不影响主流程
    }
  }
  // request 发起 HTTP 请求，回调 cb(err, {status, headers, body})；超时统一由调用方控制。
  function request(opts, cb) {
    var called = false;
    function once(err, res) {
      if (!called) {
        called = true;
        cb(err, res);
      }
    }
    try {
      if (isQX) {
        var req = { url: opts.url, method: opts.method, headers: opts.headers, body: opts.body };
        g.$task.fetch(req).then(
          function (r) { once(null, { status: r.statusCode, headers: r.headers || {}, body: r.body }); },
          function (r) { once(new Error((r && r.error) || '请求失败')); }
        );
        return;
      }
      var o = { url: opts.url, headers: opts.headers, body: opts.body };
      // Loon 的 timeout 单位是毫秒，其他客户端是秒。
      o.timeout = isLoon ? HTTP_TIMEOUT_MS : HTTP_TIMEOUT_MS / 1000;
      var method = (opts.method || 'GET').toLowerCase();
      g.$httpClient[method](o, function (err, resp, data) {
        if (err) once(new Error(String(err)));
        else once(null, { status: resp.status || resp.statusCode, headers: resp.headers || {}, body: data });
      });
    } catch (e) {
      once(e);
    }
  }
  return { isQX: isQX, isLoon: isLoon, read: read, write: write, notify: notify, request: request };
}

// parseArgs 读取订阅传入的参数：多数客户端用 $argument，QX 放在脚本地址的 # 后面。
function parseArgs(g) {
  var raw = '';
  if (typeof g.$argument === 'object' && g.$argument !== null) return g.$argument;
  if (typeof g.$argument === 'string') raw = g.$argument;
  if (!raw && g.$environment && typeof g.$environment.sourcePath === 'string') {
    var i = g.$environment.sourcePath.indexOf('#');
    if (i >= 0) raw = g.$environment.sourcePath.slice(i + 1);
  }
  return parseQuery(raw);
}

// ---------- 与服务端一致的取值逻辑 ----------

function decodeComponent(s) {
  try {
    return decodeURIComponent(s.replace(/\+/g, ' '));
  } catch (e) {
    return null;
  }
}

// parseQuery 解析 a=1&b=2，语义同 Go 的 url.ParseQuery：+ 解码为空格，没有 = 时值为空串。
function parseQuery(s) {
  var out = {};
  if (!s) return out;
  s.split('&').forEach(function (part) {
    if (!part) return;
    var i = part.indexOf('=');
    var k = decodeComponent(i >= 0 ? part.slice(0, i) : part);
    var v = decodeComponent(i >= 0 ? part.slice(i + 1) : '');
    if (k === null || v === null) return;
    if (!Object.prototype.hasOwnProperty.call(out, k)) out[k] = v;
  });
  return out;
}

// parseURL 拆出 host（小写、去掉端口和用户信息）、path（原样，空时为 /）和 query。
function parseURL(url) {
  var m = /^([a-zA-Z][a-zA-Z0-9+.-]*):\/\/([^\/?#]*)([^?#]*)(\?[^#]*)?/.exec(url || '');
  if (!m) return null;
  var authority = m[2];
  var at = authority.lastIndexOf('@');
  if (at >= 0) authority = authority.slice(at + 1);
  var host;
  if (authority.charAt(0) === '[') host = authority.slice(1, authority.indexOf(']'));
  else host = authority.split(':')[0];
  host = host.toLowerCase().replace(/\.$/, '');
  return { host: host, path: m[3] || '/', query: m[4] ? m[4].slice(1) : '' };
}

function hostMatches(pattern, host) {
  pattern = pattern.toLowerCase();
  if (pattern.indexOf('*.') === 0) {
    var suffix = '.' + pattern.slice(2);
    return host.length > suffix.length && host.slice(-suffix.length) === suffix;
  }
  return host === pattern;
}

function matchRequest(m, req) {
  var u = parseURL(req.url);
  if (!u) return false;
  if (m.method && String(m.method).toUpperCase() !== String(req.method || '').toUpperCase()) return false;
  if (!hostMatches(m.host, u.host)) return false;
  if (!m.path) return true;
  try {
    return new RegExp(m.path).test(u.path);
  } catch (e) {
    return false;
  }
}

function headerValue(headers, name) {
  var lower = name.toLowerCase();
  for (var k in headers) {
    if (Object.prototype.hasOwnProperty.call(headers, k) && k.toLowerCase() === lower) {
      var v = headers[k];
      return Array.isArray(v) ? v[0] : v;
    }
  }
  return undefined;
}

function cookieValue(headers, name) {
  var lower = 'cookie';
  for (var k in headers) {
    if (!Object.prototype.hasOwnProperty.call(headers, k) || k.toLowerCase() !== lower) continue;
    var lines = Array.isArray(headers[k]) ? headers[k] : [headers[k]];
    for (var i = 0; i < lines.length; i++) {
      var parts = String(lines[i]).split(';');
      for (var j = 0; j < parts.length; j++) {
        var p = parts[j].trim();
        var eq = p.indexOf('=');
        if (eq > 0 && p.slice(0, eq) === name) {
          var v = p.slice(eq + 1);
          if (v.length >= 2 && v.charAt(0) === '"' && v.charAt(v.length - 1) === '"') v = v.slice(1, -1);
          return v;
        }
      }
    }
  }
  return undefined;
}

// 小型 JSON 扫描器：保留数字的原文（JSON.parse 会让超过 2^53 的整数丢精度），语义同 Go 的 json.Decoder + UseNumber。
function scanJSON(text) {
  var i = 0;
  function ws() {
    while (i < text.length && ' \t\r\n'.indexOf(text.charAt(i)) >= 0) i++;
  }
  function fail() {
    throw new Error('bad json');
  }
  function value() {
    ws();
    var c = text.charAt(i);
    if (c === '{') {
      i++;
      var obj = { t: 'o', v: {} };
      ws();
      if (text.charAt(i) === '}') {
        i++;
        return obj;
      }
      for (;;) {
        ws();
        if (text.charAt(i) !== '"') fail();
        var key = str();
        ws();
        if (text.charAt(i) !== ':') fail();
        i++;
        obj.v[key] = value();
        ws();
        if (text.charAt(i) === ',') {
          i++;
          continue;
        }
        if (text.charAt(i) === '}') {
          i++;
          return obj;
        }
        fail();
      }
    }
    if (c === '[') {
      i++;
      var arr = { t: 'a', v: [] };
      ws();
      if (text.charAt(i) === ']') {
        i++;
        return arr;
      }
      for (;;) {
        arr.v.push(value());
        ws();
        if (text.charAt(i) === ',') {
          i++;
          continue;
        }
        if (text.charAt(i) === ']') {
          i++;
          return arr;
        }
        fail();
      }
    }
    if (c === '"') return { t: 's', v: str() };
    var m = /^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?/.exec(text.slice(i));
    if (m && m[0]) {
      i += m[0].length;
      return { t: 'n', v: m[0] };
    }
    if (text.slice(i, i + 4) === 'true') {
      i += 4;
      return { t: 'b', v: 'true' };
    }
    if (text.slice(i, i + 5) === 'false') {
      i += 5;
      return { t: 'b', v: 'false' };
    }
    if (text.slice(i, i + 4) === 'null') {
      i += 4;
      return { t: 'z' };
    }
    fail();
  }
  function str() {
    var m = /^"(?:[^"\\\u0000-\u001f]|\\.)*"/.exec(text.slice(i));
    if (!m) fail();
    i += m[0].length;
    return JSON.parse(m[0]);
  }
  return value();
}

// toCompact 把节点输出成紧凑 JSON：对象按键排序（同 Go 的 map 编码），数字保留原文。
function toCompact(node) {
  switch (node.t) {
    case 'o':
      return '{' + Object.keys(node.v).sort().map(function (k) { return JSON.stringify(k) + ':' + toCompact(node.v[k]); }).join(',') + '}';
    case 'a':
      return '[' + node.v.map(toCompact).join(',') + ']';
    case 's':
      return JSON.stringify(node.v);
    case 'z':
      return 'null';
    default:
      return node.v;
  }
}

function jsonPath(body, path) {
  var node;
  try {
    node = scanJSON(String(body || ''));
  } catch (e) {
    return undefined;
  }
  var segs = path.split('.');
  for (var s = 0; s < segs.length; s++) {
    if (node.t === 'o') {
      if (!Object.prototype.hasOwnProperty.call(node.v, segs[s])) return undefined;
      node = node.v[segs[s]];
    } else if (node.t === 'a') {
      if (!/^\d+$/.test(segs[s])) return undefined;
      var idx = parseInt(segs[s], 10);
      if (idx >= node.v.length) return undefined;
      node = node.v[idx];
    } else {
      return undefined;
    }
  }
  if (node.t === 'z') return undefined;
  if (node.t === 's' || node.t === 'n' || node.t === 'b') return node.v;
  return toCompact(node);
}

// extractField 返回取到的字符串；没取到返回 undefined。
function extractField(f, req) {
  var headers = req.headers || {};
  switch (f.in) {
    case 'header':
      return headerValue(headers, f.path);
    case 'cookie':
      return cookieValue(headers, f.path);
    case 'query': {
      var u = parseURL(req.url);
      if (!u) return undefined;
      var q = parseQuery(u.query);
      return Object.prototype.hasOwnProperty.call(q, f.path) ? q[f.path] : undefined;
    }
    case 'body_json':
      return jsonPath(req.body, f.path);
    case 'body_form': {
      var form = parseQuery(String(req.body || ''));
      return Object.prototype.hasOwnProperty.call(form, f.path) ? form[f.path] : undefined;
    }
    case 'regex': {
      var subject = f.regex_on === 'body' ? String(req.body || '') : String(req.url || '');
      var m;
      try {
        m = new RegExp(f.path).exec(subject);
      } catch (e) {
        return undefined;
      }
      return m && m.length > 1 && m[1] !== undefined ? m[1] : undefined;
    }
  }
  return undefined;
}

// collect 用全部规则匹配请求，返回每个命中来源取到的字段。
function collect(rules, req) {
  var out = [];
  (rules.sources || []).forEach(function (src) {
    var matched = {};
    var any = false;
    (src.matchers || []).forEach(function (m) {
      if (matchRequest(m, req)) {
        matched[m.name] = true;
        any = true;
      }
    });
    if (!any) return;
    var fields = {};
    var count = 0;
    (src.fields || []).forEach(function (f) {
      if (f.matcher && !matched[f.matcher]) return;
      var v = extractField(f, req);
      if (v !== undefined) {
        fields[f.key] = v;
        count++;
      }
    });
    if (count > 0) out.push({ source: src, matcher: Object.keys(matched)[0], fields: fields });
  });
  return out;
}

// fnv1a 是本地去重用的哈希，不出手机。
function fnv1a(s) {
  var h = 0x811c9dc5;
  for (var i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0;
  }
  return ('0000000' + h.toString(16)).slice(-8);
}

function fieldsHash(fields) {
  return fnv1a(JSON.stringify(Object.keys(fields).sort().map(function (k) { return [k, fields[k]]; })));
}

// ---------- 传输加密 ----------

// nacl 是 tweetnacl-js 1.0.3（公有领域，https://github.com/dchest/tweetnacl-js）nacl.js 第 10、32–71、77–264、469–636 行的原样摘录：
// secretbox（XSalsa20-Poly1305）与 SHA-512。外面只包了一层函数，避免和本文件的名字冲突。
// 不要手改（只在 5 行末尾加了注释检查的豁免标记，那一行的函数名会被误判成编号）；升级时整段替换，
// 再跑 internal/transport/testdata/sealed_cases.json 的共享样例。
var nacl = (function () {
'use strict';

var u64 = function(h, l) { this.hi = h|0 >>> 0; this.lo = l|0 >>> 0; };

function L32(x, c) { return (x << c) | (x >>> (32 - c)); } // lint-comments: allow

function ld32(x, i) {
  var u = x[i+3] & 0xff;
  u = (u<<8)|(x[i+2] & 0xff);
  u = (u<<8)|(x[i+1] & 0xff);
  return (u<<8)|(x[i+0] & 0xff);
}

function dl64(x, i) {
  var h = (x[i] << 24) | (x[i+1] << 16) | (x[i+2] << 8) | x[i+3];
  var l = (x[i+4] << 24) | (x[i+5] << 16) | (x[i+6] << 8) | x[i+7];
  return new u64(h, l);
}

function st32(x, j, u) {
  var i;
  for (i = 0; i < 4; i++) { x[j+i] = u & 255; u >>>= 8; }
}

function ts64(x, i, u) {
  x[i]   = (u.hi >> 24) & 0xff;
  x[i+1] = (u.hi >> 16) & 0xff;
  x[i+2] = (u.hi >>  8) & 0xff;
  x[i+3] = u.hi & 0xff;
  x[i+4] = (u.lo >> 24)  & 0xff;
  x[i+5] = (u.lo >> 16)  & 0xff;
  x[i+6] = (u.lo >>  8)  & 0xff;
  x[i+7] = u.lo & 0xff;
}

function vn(x, xi, y, yi, n) {
  var i,d = 0;
  for (i = 0; i < n; i++) d |= x[xi+i]^y[yi+i];
  return (1 & ((d - 1) >>> 8)) - 1;
}

function crypto_verify_16(x, xi, y, yi) {
  return vn(x,xi,y,yi,16);
}

function core(out,inp,k,c,h) {
  var w = new Uint32Array(16), x = new Uint32Array(16),
      y = new Uint32Array(16), t = new Uint32Array(4);
  var i, j, m;

  for (i = 0; i < 4; i++) {
    x[5*i] = ld32(c, 4*i);
    x[1+i] = ld32(k, 4*i);
    x[6+i] = ld32(inp, 4*i);
    x[11+i] = ld32(k, 16+4*i);
  }

  for (i = 0; i < 16; i++) y[i] = x[i];

  for (i = 0; i < 20; i++) {
    for (j = 0; j < 4; j++) {
      for (m = 0; m < 4; m++) t[m] = x[(5*j+4*m)%16];
      t[1] ^= L32((t[0]+t[3])|0, 7); // lint-comments: allow
      t[2] ^= L32((t[1]+t[0])|0, 9); // lint-comments: allow
      t[3] ^= L32((t[2]+t[1])|0,13); // lint-comments: allow
      t[0] ^= L32((t[3]+t[2])|0,18); // lint-comments: allow
      for (m = 0; m < 4; m++) w[4*j+(j+m)%4] = t[m];
    }
    for (m = 0; m < 16; m++) x[m] = w[m];
  }

  if (h) {
    for (i = 0; i < 16; i++) x[i] = (x[i] + y[i]) | 0;
    for (i = 0; i < 4; i++) {
      x[5*i] = (x[5*i] - ld32(c, 4*i)) | 0;
      x[6+i] = (x[6+i] - ld32(inp, 4*i)) | 0;
    }
    for (i = 0; i < 4; i++) {
      st32(out,4*i,x[5*i]);
      st32(out,16+4*i,x[6+i]);
    }
  } else {
    for (i = 0; i < 16; i++) st32(out, 4 * i, (x[i] + y[i]) | 0);
  }
}

function crypto_core_salsa20(out,inp,k,c) {
  core(out,inp,k,c,false);
  return 0;
}

function crypto_core_hsalsa20(out,inp,k,c) {
  core(out,inp,k,c,true);
  return 0;
}

var sigma = new Uint8Array([101, 120, 112, 97, 110, 100, 32, 51, 50, 45, 98, 121, 116, 101, 32, 107]);
            // "expand 32-byte k"

function crypto_stream_salsa20_xor(c,cpos,m,mpos,b,n,k) {
  var z = new Uint8Array(16), x = new Uint8Array(64);
  var u, i;
  if (!b) return 0;
  for (i = 0; i < 16; i++) z[i] = 0;
  for (i = 0; i < 8; i++) z[i] = n[i];
  while (b >= 64) {
    crypto_core_salsa20(x,z,k,sigma);
    for (i = 0; i < 64; i++) c[cpos+i] = (m?m[mpos+i]:0) ^ x[i];
    u = 1;
    for (i = 8; i < 16; i++) {
      u = u + (z[i] & 0xff) | 0;
      z[i] = u & 0xff;
      u >>>= 8;
    }
    b -= 64;
    cpos += 64;
    if (m) mpos += 64;
  }
  if (b > 0) {
    crypto_core_salsa20(x,z,k,sigma);
    for (i = 0; i < b; i++) c[cpos+i] = (m?m[mpos+i]:0) ^ x[i];
  }
  return 0;
}

function crypto_stream_salsa20(c,cpos,d,n,k) {
  return crypto_stream_salsa20_xor(c,cpos,null,0,d,n,k);
}

function crypto_stream(c,cpos,d,n,k) {
  var s = new Uint8Array(32);
  crypto_core_hsalsa20(s,n,k,sigma);
  return crypto_stream_salsa20(c,cpos,d,n.subarray(16),s);
}

function crypto_stream_xor(c,cpos,m,mpos,d,n,k) {
  var s = new Uint8Array(32);
  crypto_core_hsalsa20(s,n,k,sigma);
  return crypto_stream_salsa20_xor(c,cpos,m,mpos,d,n.subarray(16),s);
}

function add1305(h, c) {
  var j, u = 0;
  for (j = 0; j < 17; j++) {
    u = (u + ((h[j] + c[j]) | 0)) | 0;
    h[j] = u & 255;
    u >>>= 8;
  }
}

var minusp = new Uint32Array([
  5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 252
]);

function crypto_onetimeauth(out, outpos, m, mpos, n, k) {
  var s, i, j, u;
  var x = new Uint32Array(17), r = new Uint32Array(17),
      h = new Uint32Array(17), c = new Uint32Array(17),
      g = new Uint32Array(17);
  for (j = 0; j < 17; j++) r[j]=h[j]=0;
  for (j = 0; j < 16; j++) r[j]=k[j];
  r[3]&=15;
  r[4]&=252;
  r[7]&=15;
  r[8]&=252;
  r[11]&=15;
  r[12]&=252;
  r[15]&=15;

  while (n > 0) {
    for (j = 0; j < 17; j++) c[j] = 0;
    for (j = 0; (j < 16) && (j < n); ++j) c[j] = m[mpos+j];
    c[j] = 1;
    mpos += j; n -= j;
    add1305(h,c);
    for (i = 0; i < 17; i++) {
      x[i] = 0;
      for (j = 0; j < 17; j++) x[i] = (x[i] + (h[j] * ((j <= i) ? r[i - j] : ((320 * r[i + 17 - j])|0))) | 0) | 0;
    }
    for (i = 0; i < 17; i++) h[i] = x[i];
    u = 0;
    for (j = 0; j < 16; j++) {
      u = (u + h[j]) | 0;
      h[j] = u & 255;
      u >>>= 8;
    }
    u = (u + h[16]) | 0; h[16] = u & 3;
    u = (5 * (u >>> 2)) | 0;
    for (j = 0; j < 16; j++) {
      u = (u + h[j]) | 0;
      h[j] = u & 255;
      u >>>= 8;
    }
    u = (u + h[16]) | 0; h[16] = u;
  }

  for (j = 0; j < 17; j++) g[j] = h[j];
  add1305(h,minusp);
  s = (-(h[16] >>> 7) | 0);
  for (j = 0; j < 17; j++) h[j] ^= s & (g[j] ^ h[j]);

  for (j = 0; j < 16; j++) c[j] = k[j + 16];
  c[16] = 0;
  add1305(h,c);
  for (j = 0; j < 16; j++) out[outpos+j] = h[j];
  return 0;
}

function crypto_onetimeauth_verify(h, hpos, m, mpos, n, k) {
  var x = new Uint8Array(16);
  crypto_onetimeauth(x,0,m,mpos,n,k);
  return crypto_verify_16(h,hpos,x,0);
}

function crypto_secretbox(c,m,d,n,k) {
  var i;
  if (d < 32) return -1;
  crypto_stream_xor(c,0,m,0,d,n,k);
  crypto_onetimeauth(c, 16, c, 32, d - 32, c);
  for (i = 0; i < 16; i++) c[i] = 0;
  return 0;
}

function crypto_secretbox_open(m,c,d,n,k) {
  var i;
  var x = new Uint8Array(32);
  if (d < 32) return -1;
  crypto_stream(x,0,32,n,k);
  if (crypto_onetimeauth_verify(c, 16,c, 32,d - 32,x) !== 0) return -1;
  crypto_stream_xor(m,0,c,0,d,n,k);
  for (i = 0; i < 32; i++) m[i] = 0;
  return 0;
}

function add64() {
  var a = 0, b = 0, c = 0, d = 0, m16 = 65535, l, h, i;
  for (i = 0; i < arguments.length; i++) {
    l = arguments[i].lo;
    h = arguments[i].hi;
    a += (l & m16); b += (l >>> 16);
    c += (h & m16); d += (h >>> 16);
  }

  b += (a >>> 16);
  c += (b >>> 16);
  d += (c >>> 16);

  return new u64((c & m16) | (d << 16), (a & m16) | (b << 16));
}

function shr64(x, c) {
  return new u64((x.hi >>> c), (x.lo >>> c) | (x.hi << (32 - c)));
}

function xor64() {
  var l = 0, h = 0, i;
  for (i = 0; i < arguments.length; i++) {
    l ^= arguments[i].lo;
    h ^= arguments[i].hi;
  }
  return new u64(h, l);
}

function R(x, c) {
  var h, l, c1 = 32 - c;
  if (c < 32) {
    h = (x.hi >>> c) | (x.lo << c1);
    l = (x.lo >>> c) | (x.hi << c1);
  } else if (c < 64) {
    h = (x.lo >>> c) | (x.hi << c1);
    l = (x.hi >>> c) | (x.lo << c1);
  }
  return new u64(h, l);
}

function Ch(x, y, z) {
  var h = (x.hi & y.hi) ^ (~x.hi & z.hi),
      l = (x.lo & y.lo) ^ (~x.lo & z.lo);
  return new u64(h, l);
}

function Maj(x, y, z) {
  var h = (x.hi & y.hi) ^ (x.hi & z.hi) ^ (y.hi & z.hi),
      l = (x.lo & y.lo) ^ (x.lo & z.lo) ^ (y.lo & z.lo);
  return new u64(h, l);
}

function Sigma0(x) { return xor64(R(x,28), R(x,34), R(x,39)); }
function Sigma1(x) { return xor64(R(x,14), R(x,18), R(x,41)); }
function sigma0(x) { return xor64(R(x, 1), R(x, 8), shr64(x,7)); }
function sigma1(x) { return xor64(R(x,19), R(x,61), shr64(x,6)); }

var K = [
  new u64(0x428a2f98, 0xd728ae22), new u64(0x71374491, 0x23ef65cd),
  new u64(0xb5c0fbcf, 0xec4d3b2f), new u64(0xe9b5dba5, 0x8189dbbc),
  new u64(0x3956c25b, 0xf348b538), new u64(0x59f111f1, 0xb605d019),
  new u64(0x923f82a4, 0xaf194f9b), new u64(0xab1c5ed5, 0xda6d8118),
  new u64(0xd807aa98, 0xa3030242), new u64(0x12835b01, 0x45706fbe),
  new u64(0x243185be, 0x4ee4b28c), new u64(0x550c7dc3, 0xd5ffb4e2),
  new u64(0x72be5d74, 0xf27b896f), new u64(0x80deb1fe, 0x3b1696b1),
  new u64(0x9bdc06a7, 0x25c71235), new u64(0xc19bf174, 0xcf692694),
  new u64(0xe49b69c1, 0x9ef14ad2), new u64(0xefbe4786, 0x384f25e3),
  new u64(0x0fc19dc6, 0x8b8cd5b5), new u64(0x240ca1cc, 0x77ac9c65),
  new u64(0x2de92c6f, 0x592b0275), new u64(0x4a7484aa, 0x6ea6e483),
  new u64(0x5cb0a9dc, 0xbd41fbd4), new u64(0x76f988da, 0x831153b5),
  new u64(0x983e5152, 0xee66dfab), new u64(0xa831c66d, 0x2db43210),
  new u64(0xb00327c8, 0x98fb213f), new u64(0xbf597fc7, 0xbeef0ee4),
  new u64(0xc6e00bf3, 0x3da88fc2), new u64(0xd5a79147, 0x930aa725),
  new u64(0x06ca6351, 0xe003826f), new u64(0x14292967, 0x0a0e6e70),
  new u64(0x27b70a85, 0x46d22ffc), new u64(0x2e1b2138, 0x5c26c926),
  new u64(0x4d2c6dfc, 0x5ac42aed), new u64(0x53380d13, 0x9d95b3df),
  new u64(0x650a7354, 0x8baf63de), new u64(0x766a0abb, 0x3c77b2a8),
  new u64(0x81c2c92e, 0x47edaee6), new u64(0x92722c85, 0x1482353b),
  new u64(0xa2bfe8a1, 0x4cf10364), new u64(0xa81a664b, 0xbc423001),
  new u64(0xc24b8b70, 0xd0f89791), new u64(0xc76c51a3, 0x0654be30),
  new u64(0xd192e819, 0xd6ef5218), new u64(0xd6990624, 0x5565a910),
  new u64(0xf40e3585, 0x5771202a), new u64(0x106aa070, 0x32bbd1b8),
  new u64(0x19a4c116, 0xb8d2d0c8), new u64(0x1e376c08, 0x5141ab53),
  new u64(0x2748774c, 0xdf8eeb99), new u64(0x34b0bcb5, 0xe19b48a8),
  new u64(0x391c0cb3, 0xc5c95a63), new u64(0x4ed8aa4a, 0xe3418acb),
  new u64(0x5b9cca4f, 0x7763e373), new u64(0x682e6ff3, 0xd6b2b8a3),
  new u64(0x748f82ee, 0x5defb2fc), new u64(0x78a5636f, 0x43172f60),
  new u64(0x84c87814, 0xa1f0ab72), new u64(0x8cc70208, 0x1a6439ec),
  new u64(0x90befffa, 0x23631e28), new u64(0xa4506ceb, 0xde82bde9),
  new u64(0xbef9a3f7, 0xb2c67915), new u64(0xc67178f2, 0xe372532b),
  new u64(0xca273ece, 0xea26619c), new u64(0xd186b8c7, 0x21c0c207),
  new u64(0xeada7dd6, 0xcde0eb1e), new u64(0xf57d4f7f, 0xee6ed178),
  new u64(0x06f067aa, 0x72176fba), new u64(0x0a637dc5, 0xa2c898a6),
  new u64(0x113f9804, 0xbef90dae), new u64(0x1b710b35, 0x131c471b),
  new u64(0x28db77f5, 0x23047d84), new u64(0x32caab7b, 0x40c72493),
  new u64(0x3c9ebe0a, 0x15c9bebc), new u64(0x431d67c4, 0x9c100d4c),
  new u64(0x4cc5d4be, 0xcb3e42b6), new u64(0x597f299c, 0xfc657e2a),
  new u64(0x5fcb6fab, 0x3ad6faec), new u64(0x6c44198c, 0x4a475817)
];

function crypto_hashblocks(x, m, n) {
  var z = [], b = [], a = [], w = [], t, i, j;

  for (i = 0; i < 8; i++) z[i] = a[i] = dl64(x, 8*i);

  var pos = 0;
  while (n >= 128) {
    for (i = 0; i < 16; i++) w[i] = dl64(m, 8*i+pos);
    for (i = 0; i < 80; i++) {
      for (j = 0; j < 8; j++) b[j] = a[j];
      t = add64(a[7], Sigma1(a[4]), Ch(a[4], a[5], a[6]), K[i], w[i%16]);
      b[7] = add64(t, Sigma0(a[0]), Maj(a[0], a[1], a[2]));
      b[3] = add64(b[3], t);
      for (j = 0; j < 8; j++) a[(j+1)%8] = b[j];
      if (i%16 === 15) {
        for (j = 0; j < 16; j++) {
          w[j] = add64(w[j], w[(j+9)%16], sigma0(w[(j+1)%16]), sigma1(w[(j+14)%16]));
        }
      }
    }

    for (i = 0; i < 8; i++) {
      a[i] = add64(a[i], z[i]);
      z[i] = a[i];
    }

    pos += 128;
    n -= 128;
  }

  for (i = 0; i < 8; i++) ts64(x, 8*i, z[i]);
  return n;
}

var iv = new Uint8Array([
  0x6a,0x09,0xe6,0x67,0xf3,0xbc,0xc9,0x08,
  0xbb,0x67,0xae,0x85,0x84,0xca,0xa7,0x3b,
  0x3c,0x6e,0xf3,0x72,0xfe,0x94,0xf8,0x2b,
  0xa5,0x4f,0xf5,0x3a,0x5f,0x1d,0x36,0xf1,
  0x51,0x0e,0x52,0x7f,0xad,0xe6,0x82,0xd1,
  0x9b,0x05,0x68,0x8c,0x2b,0x3e,0x6c,0x1f,
  0x1f,0x83,0xd9,0xab,0xfb,0x41,0xbd,0x6b,
  0x5b,0xe0,0xcd,0x19,0x13,0x7e,0x21,0x79
]);

function crypto_hash(out, m, n) {
  var h = new Uint8Array(64), x = new Uint8Array(256);
  var i, b = n;

  for (i = 0; i < 64; i++) h[i] = iv[i];

  crypto_hashblocks(h, m, n);
  n %= 128;

  for (i = 0; i < 256; i++) x[i] = 0;
  for (i = 0; i < n; i++) x[i] = m[b-n+i];
  x[n] = 128;

  n = 256-128*(n<112?1:0);
  x[n-9] = 0;
  ts64(x, n-8, new u64((b / 0x20000000) | 0, b << 3));
  crypto_hashblocks(h, x, n);

  for (i = 0; i < 64; i++) out[i] = h[i];

  return 0;
}

return { secretbox: crypto_secretbox, secretboxOpen: crypto_secretbox_open, hash: crypto_hash };
})();

// 报文格式与服务端 internal/transport 一致："f1." + base64url(nonce ‖ secretbox 输出)，不带填充。
var TRANSPORT_PREFIX = 'f1.';
var B64URL = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

// utf8Encode 把字符串编码成 UTF-8 字节；有的引擎没有 TextEncoder。落单的代理项按 U+FFFD 处理，同 TextEncoder。
function utf8Encode(s) {
  var out = [];
  for (var i = 0; i < s.length; i++) {
    var c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
      var d = s.charCodeAt(i + 1);
      if (d >= 0xdc00 && d <= 0xdfff) {
        c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00);
        i++;
      }
    }
    if (c >= 0xd800 && c <= 0xdfff) c = 0xfffd;
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
  }
  return new Uint8Array(out);
}

// utf8Decode 严格解码 UTF-8，不合法时返回 null。
function utf8Decode(b) {
  var s = '';
  var i = 0;
  while (i < b.length) {
    var c = b[i];
    var n = c < 0x80 ? 0 : c >= 0xc2 && c < 0xe0 ? 1 : c >= 0xe0 && c < 0xf0 ? 2 : c >= 0xf0 && c < 0xf5 ? 3 : -1;
    if (n < 0 || i + n >= b.length) return null;
    var cp = n === 0 ? c : c & (0x3f >> n);
    for (var j = 1; j <= n; j++) {
      var t = b[i + j];
      if ((t & 0xc0) !== 0x80) return null;
      cp = (cp << 6) | (t & 63);
    }
    if ((n === 2 && (cp < 0x800 || (cp >= 0xd800 && cp <= 0xdfff))) || (n === 3 && (cp < 0x10000 || cp > 0x10ffff))) return null;
    if (cp >= 0x10000) {
      cp -= 0x10000;
      s += String.fromCharCode(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff));
    } else {
      s += String.fromCharCode(cp);
    }
    i += n + 1;
  }
  return s;
}

function b64urlEncode(b) {
  var s = '';
  for (var i = 0; i < b.length; i += 3) {
    var n = (b[i] << 16) | ((i + 1 < b.length ? b[i + 1] : 0) << 8) | (i + 2 < b.length ? b[i + 2] : 0);
    s += B64URL.charAt((n >> 18) & 63) + B64URL.charAt((n >> 12) & 63);
    if (i + 1 < b.length) s += B64URL.charAt((n >> 6) & 63);
    if (i + 2 < b.length) s += B64URL.charAt(n & 63);
  }
  return s;
}

// b64urlDecode 严格解码不带填充的 base64url：有非法字符、长度不对或末尾多余位不为零时返回 null，和服务端一致。
function b64urlDecode(s) {
  if (s.length % 4 === 1) return null;
  var out = new Uint8Array(Math.floor((s.length * 3) / 4));
  var acc = 0;
  var bits = 0;
  var o = 0;
  for (var i = 0; i < s.length; i++) {
    var v = B64URL.indexOf(s.charAt(i));
    if (v < 0) return null;
    acc = ((acc << 6) | v) & 0xffffff;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[o++] = (acc >> bits) & 255;
    }
  }
  if (acc & ((1 << bits) - 1)) return null;
  return out;
}

function concatBytes(a, b) {
  var out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

// transportKeys 解析模块参数里的传输密钥，派生请求（up）和回复（down）两个子密钥；密钥不合法时返回 null。
function transportKeys(text) {
  var key = b64urlDecode(String(text || ''));
  if (!key || key.length !== 32) return null;
  function sub(label) {
    var m = concatBytes(utf8Encode(label), key);
    var h = new Uint8Array(64);
    nacl.hash(h, m, m.length);
    return h.subarray(0, 32);
  }
  return { up: sub('ferry/v1/up'), down: sub('ferry/v1/down') };
}

// sealMessage 用子密钥加密字符串。nonce 是 24 字节，对同一把密钥不能重复使用。
function sealMessage(subkey, nonce, plaintext) {
  var p = utf8Encode(plaintext);
  var m = new Uint8Array(32 + p.length);
  m.set(p, 32);
  var c = new Uint8Array(m.length);
  nacl.secretbox(c, m, m.length, nonce, subkey);
  return TRANSPORT_PREFIX + b64urlEncode(concatBytes(nonce, c.subarray(16)));
}

// openMessage 解开报文，返回 { nonce, plaintext }；格式不对、被篡改或密钥不对时返回 null。
function openMessage(subkey, msg) {
  msg = String(msg || '');
  if (msg.indexOf(TRANSPORT_PREFIX) !== 0) return null;
  var raw = b64urlDecode(msg.slice(TRANSPORT_PREFIX.length));
  if (!raw || raw.length < 24 + 16) return null;
  var nonce = raw.subarray(0, 24);
  var c = new Uint8Array(16 + raw.length - 24);
  c.set(raw.subarray(24), 16);
  var m = new Uint8Array(c.length);
  if (nacl.secretboxOpen(m, c, c.length, nonce, subkey) !== 0) return null;
  var text = utf8Decode(m.subarray(32));
  if (text === null) return null;
  return { nonce: new Uint8Array(nonce), plaintext: text };
}

// ---------- 主流程 ----------

function run(g) {
  var finished = false;
  function done() {
    if (finished) return;
    finished = true;
    g.$done({});
  }
  var safety = g.setTimeout(done, SAFETY_TIMEOUT_MS);
  function finish() {
    try {
      if (g.clearTimeout) g.clearTimeout(safety);
    } catch (e) {
      // 忽略
    }
    done();
  }

  try {
    var env = makeEnv(g);
    var args = parseArgs(g);
    if (!args.base || typeof g.$request === 'undefined') return finish();
    var channel = openChannel(g, env, args);
    if (!channel) return finish();
    var req = { method: g.$request.method || 'GET', url: g.$request.url, headers: g.$request.headers || {}, body: g.$request.body };

    channel.ready(function () {
      loadRules(env, channel, function (rules) {
        try {
          if (!rules) return finish();
          var hits = collect(rules, req);
          sendAll(env, channel, hits, finish);
        } catch (e) {
          finish();
        }
      });
    });
  } catch (e) {
    finish();
  }
}

// withTimeout 给请求加统一的超时，超时后忽略迟到的回调。
function withTimeout(g, env, opts, cb) {
  var called = false;
  var timer = g.setTimeout(function () {
    if (!called) {
      called = true;
      cb(new Error('请求超时'));
    }
  }, HTTP_TIMEOUT_MS);
  env.request(opts, function (err, res) {
    if (called) return;
    called = true;
    try {
      if (g.clearTimeout) g.clearTimeout(timer);
    } catch (e) {
      // 忽略
    }
    cb(err, res);
  });
}

// requestWithFallback 先用主地址，网络失败时再试备用地址。
function requestWithFallback(g, env, args, path, opts, cb) {
  var bases = [args.base];
  if (args.fallback) bases.push(args.fallback);
  var i = 0;
  function next() {
    var o = {};
    for (var k in opts) o[k] = opts[k];
    o.url = bases[i] + path;
    withTimeout(g, env, o, function (err, res) {
      if (err && ++i < bases.length) return next();
      cb(err, res);
    });
  }
  next();
}

function readJSON(env, key) {
  var raw = env.read(key);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch (e) {
    return null;
  }
}

// ---------- 通道：明文或加密 ----------
//
// 两种通道对外一样：call(op, body, etag, cb)，op 是 ping、rules 或 sync，回调 cb(err, { status, etag, body })。
// err 只表示网络不通或回复校验失败；平台返回的错误放在 status 和 body.error 里。

// openChannel 按参数和本地记录选通道。本地记过“走过加密”后只走加密，不看参数怎么写，
// 这样即使订阅文件被改、key 参数被去掉，也不会退回明文。拿不到能用的通道时返回 null，由调用方直接放行。
function openChannel(g, env, args) {
  // 客户端没替换占位符时（{{{key}}}、${args.key}、[{key}]），按没填处理。
  var key = /\{\{|\}\}|\$\{|\{key\}/.test(args.key || '') ? '' : String(args.key || '').trim();
  var sealed = !!key || args.mode === 'sealed' || env.read(SEALED_KEY) === '1';
  if (!sealed) return args.token ? plainChannel(g, env, args) : null;
  if (!args.kid) return null;
  if (key) env.write('1', SEALED_KEY);
  var keys = key ? transportKeys(key) : null;
  if (!keys) {
    warnOnce(env, 'nokey', '尚未填写传输密钥', '请在代理客户端的模块参数中填写 Ferry 后台显示的传输密钥，填写前不回传登录态。');
    return null;
  }
  return sealedChannel(g, env, args, keys, fnv1a(args.kid + ':' + key));
}

function plainChannel(g, env, args) {
  return {
    sealed: false,
    ready: function (cb) {
      cb();
    },
    call: function (op, body, etag, cb) {
      var headers = { 'X-Ferry-Subscription': args.token };
      var path = '/api/v1/rules';
      var opts = { method: 'GET', headers: headers };
      if (op === 'rules') {
        if (etag) headers['If-None-Match'] = etag;
      } else {
        path = '/api/v1/sync';
        headers['Content-Type'] = 'application/json';
        opts = { method: 'POST', headers: headers, body: JSON.stringify(body) };
      }
      requestWithFallback(g, env, args, path, opts, function (err, res) {
        if (err) return cb(err);
        var parsed = null;
        if (res.status !== 304) {
          try {
            parsed = JSON.parse(res.body);
          } catch (e) {
            parsed = null;
          }
        }
        cb(null, { status: res.status, etag: headerValue(res.headers || {}, 'ETag') || '', body: parsed });
      });
    },
  };
}

// sealedChannel 用传输密钥加密整个请求，发到 /api/v1/sealed；回复要能解开，并且对应的是这条请求。
// fp 是“编号 + 密钥”的指纹：和上次连通时不同（第一次填、换了密钥）就先发一次 ping，在手机上告诉用户填得对不对。
function sealedChannel(g, env, args, keys, fp) {
  function call(op, body, etag, cb) {
    var req = { v: 1, op: op, ts: Date.now() };
    if (body !== undefined) req.body = body;
    if (etag) req.etag = etag;
    var nonce = makeNonce(g, env);
    var msg = sealMessage(keys.up, nonce, padJSON(JSON.stringify(req)));
    var headers = { 'X-Ferry-Kid': args.kid, 'Content-Type': 'text/plain' };
    requestWithFallback(g, env, args, '/api/v1/sealed', { method: 'POST', headers: headers, body: msg }, function (err, res) {
      if (err) return cb(err);
      if (res.status === 401) {
        warnOnce(env, 'badkey', '传输密钥错误', '服务端无法解密请求：密钥填写错误或已在后台更换，请在模块参数中填写后台显示的密钥。');
        return cb(null, { status: 401, etag: '', body: { error: '传输密钥错误或已更换' }, warned: true });
      }
      if (res.status !== 200) return cb(null, { status: res.status, etag: '', body: null });
      var opened = openMessage(keys.down, res.body);
      var rep = null;
      try {
        rep = opened && JSON.parse(opened.plaintext);
      } catch (e) {
        rep = null;
      }
      if (!rep || rep.re !== b64urlEncode(nonce)) return cb(new Error('响应校验失败'));
      var out = { status: rep.status, etag: rep.etag || '', body: rep.body === undefined ? null : rep.body };
      if (rep.status === 409 && rep.time) {
        out.warned = true;
        warnOnce(env, 'clock', '设备时间不准确', '设备时间与服务端时间相差超过 5 分钟，请开启自动设置时间。');
      } else if (rep.status === 410) {
        out.warned = true;
        env.write('', SEALED_KEY);
        env.write('', KEY_FP_KEY);
        warnOnce(env, 'gone', '订阅已切换为明文传输', '请在代理客户端中重新安装 Ferry 订阅。');
      }
      cb(null, out);
    });
  }
  return {
    sealed: true,
    ready: function (cb) {
      if (env.read(KEY_FP_KEY) === fp) return cb();
      call('ping', undefined, '', function (err, res) {
        if (!err && res.status === 200) {
          env.write(fp, KEY_FP_KEY);
          env.notify('Ferry', '传输已加密', '已使用模块参数中的密钥连接服务端，后续回传均加密传输。');
        } else if (err) {
          env.notify('Ferry', '加密连接失败', err.message);
        }
        cb();
      });
    },
    call: call,
  };
}

// warnOnce 同一类提示每小时最多发一次，免得每个请求都弹通知。
function warnOnce(env, kind, subtitle, body) {
  var key = WARN_KEY_PREFIX + kind;
  var now = Date.now();
  var last = Number(env.read(key) || 0);
  if (now - last < WARN_INTERVAL_MS) return;
  env.write(String(now), key);
  env.notify('Ferry', subtitle, body);
}

// padJSON 在 JSON 后面补空格，让 UTF-8 长度是 256 的整数倍，减少按长度猜内容的余地。
function padJSON(s) {
  var n = utf8Encode(s).length % 256;
  return n === 0 ? s : s + new Array(256 - n + 1).join(' ');
}

// makeNonce 生成 24 字节 nonce。有安全随机数时直接用；没有时用“毫秒时间 + 持久化计数器 + Math.random”拼，
// XSalsa20 只要求同一把密钥下 nonce 不重复，不要求不可预测。
function makeNonce(g, env) {
  var n = new Uint8Array(24);
  try {
    if (g.crypto && typeof g.crypto.getRandomValues === 'function') {
      g.crypto.getRandomValues(n);
      return n;
    }
  } catch (e) {
    // 落到下面的拼法
  }
  var t = Date.now();
  for (var i = 5; i >= 0; i--) {
    n[i] = t % 256;
    t = Math.floor(t / 256);
  }
  var c = (Number(env.read(NONCE_COUNTER_KEY) || 0) + 1) >>> 0;
  env.write(String(c), NONCE_COUNTER_KEY);
  for (var j = 9; j >= 6; j--) {
    n[j] = c & 255;
    c >>>= 8;
  }
  for (var k = 10; k < 24; k++) n[k] = Math.floor(Math.random() * 256);
  return n;
}

function loadRules(env, channel, cb) {
  var cache = readJSON(env, RULES_KEY);
  var now = Date.now();
  if (cache && cache.rules && now - cache.fetchedAt < RULES_TTL_MS) return cb(cache.rules);
  channel.call('rules', undefined, cache && cache.etag, function (err, res) {
    if (!err && res.status === 304 && cache) {
      cache.fetchedAt = now;
      env.write(JSON.stringify(cache), RULES_KEY);
      return cb(cache.rules);
    }
    if (!err && res.status === 200 && res.body) {
      env.write(JSON.stringify({ etag: res.etag, fetchedAt: now, rules: res.body }), RULES_KEY);
      return cb(res.body);
    }
    cb(cache ? cache.rules : null);
  });
}

function sendAll(env, channel, hits, finish) {
  var pending = hits.length;
  if (pending === 0) return finish();
  hits.forEach(function (hit) {
    sendOne(env, channel, hit, function () {
      if (--pending === 0) finish();
    });
  });
}

function sendOne(env, channel, hit, cb) {
  var src = hit.source;
  var lastKey = LAST_KEY_PREFIX + src.key;
  var last = readJSON(env, lastKey);
  var hash = fieldsHash(hit.fields);
  var now = Date.now();
  if (last && last.hash === hash) return cb();
  if (last && src.min_interval && now - last.at < src.min_interval * 1000) return cb();

  var body = { source: src.key, matcher: hit.matcher, fields: hit.fields, seen_at: Math.floor(now / 1000) };
  var title = 'Ferry · ' + (src.name || src.key);
  var keys = Object.keys(hit.fields).join('、');
  channel.call('sync', body, '', function (err, res) {
    var result = null;
    if (!err && res.status >= 200 && res.status < 300) {
      result = res.body || { result: 'accepted' };
      env.write(JSON.stringify({ hash: hash, at: now }), lastKey);
    }
    var mode = src.notify || 'always';
    // 通知只写字段名，不写值。通道已经单独提示过的错误（密钥不对、时间不准等）不再重复通知。
    if (!result) {
      var reason = err ? err.message : (res.body && res.body.error) || 'HTTP ' + res.status;
      if (mode === 'always' && !(res && res.warned)) env.notify(title, '回传失败', reason + '；字段：' + keys);
    } else if (mode === 'always' || (mode === 'first' && !last) || (mode === 'changed' && result.result === 'accepted')) {
      env.notify(title, result.message || '已回传', '字段：' + keys);
    }
    cb();
  });
}

function globals() {
  return {
    $request: typeof $request !== 'undefined' ? $request : undefined,
    $argument: typeof $argument !== 'undefined' ? $argument : undefined,
    $environment: typeof $environment !== 'undefined' ? $environment : undefined,
    $done: typeof $done !== 'undefined' ? $done : function () {},
    $httpClient: typeof $httpClient !== 'undefined' ? $httpClient : undefined,
    $persistentStore: typeof $persistentStore !== 'undefined' ? $persistentStore : undefined,
    $notification: typeof $notification !== 'undefined' ? $notification : undefined,
    $task: typeof $task !== 'undefined' ? $task : undefined,
    $prefs: typeof $prefs !== 'undefined' ? $prefs : undefined,
    $notify: typeof $notify !== 'undefined' ? $notify : undefined,
    $loon: typeof $loon !== 'undefined' ? $loon : undefined,
    crypto: typeof crypto !== 'undefined' ? crypto : undefined,
    setTimeout: setTimeout,
    clearTimeout: typeof clearTimeout !== 'undefined' ? clearTimeout : undefined,
  };
}

// 测试时由测试代码设置 __FERRY_TEST__，只导出函数、不执行。
if (typeof __FERRY_TEST__ !== 'undefined') {
  __FERRY_TEST__.exports = {
    matchRequest: matchRequest,
    extractField: extractField,
    collect: collect,
    run: run,
    parseArgs: parseArgs,
    utf8Encode: utf8Encode,
    utf8Decode: utf8Decode,
    b64urlDecode: b64urlDecode,
    transportKeys: transportKeys,
    sealMessage: sealMessage,
    openMessage: openMessage,
    b64urlEncode: b64urlEncode,
    makeNonce: makeNonce,
  };
} else {
  run(globals());
}
