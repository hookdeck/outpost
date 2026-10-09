// Golden vector generator for internal/mcpevents. canonicalJson and
// deriveSubscriptionId are copied verbatim (types stripped) from the MCP Events
// Outpost demo: src/shared/canonical-json.ts and src/server/identity.ts.
// URL vectors record WHATWG `new URL(input).href`.
// Usage (node 24): node gen.mjs internal/mcpevents/testdata
import { createHash } from 'node:crypto';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';

function canonicalJson(value) {
  if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'null';
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(',')}]`;
  const record = value;
  const members = Object.keys(record)
    .filter((key) => record[key] !== undefined)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${canonicalJson(record[key])}`);
  return `{${members.join(',')}}`;
}

const sha256 = (value) => createHash('sha256').update(value).digest('hex');

function deriveSubscriptionId(principal, url, name, args) {
  return `sub_${sha256(canonicalJson({ principal, url, name, arguments: args })).slice(0, 32)}`;
}

const outDir = process.argv[2] ?? '.';

// ---- canonical JSON: inputs are JSON texts, parsed with JSON.parse as an
// MCP server would parse a request body.
const canonicalInputs = [
  ['empty object', '{}'],
  ['empty array', '[]'],
  ['null', 'null'],
  ['true', 'true'],
  ['false', 'false'],
  ['empty string', '""'],
  ['key order', '{"b":1,"a":2,"c":{"z":1,"y":[3,2,1],"x":{}}}'],
  ['integer-like keys', '{"10":1,"9":2,"1":3,"a":4,"":5,"_":6,"A":7}'],
  ['utf16 key order', '{"\uff01":1,"\u{1f600}":2,"\u00e9":3,"e":4,"\u2028":5,"z":6}'],
  ['proto key', '{"__proto__":{"a":1},"constructor":2}'],
  ['nested arrays', '[[[]],[[1,[2,[3]]]],{"a":[{"c":1,"b":2}]}]'],
  ['escapes', '"quote\\" backslash\\\\ slash/ tab\\t nl\\n cr\\r bs\\b ff\\f"'],
  ['controls', '"\\u0000\\u0001\\u0007\\u000b\\u000e\\u001f\\u0020\\u007f"'],
  ['line separators raw', '"a\\u2028b\\u2029c"'],
  ['html chars', '"<script>&amp;\'</script>"'],
  ['unicode', '"caf\\u00e9 \\u65e5\\u672c \\ud83d\\ude00 \\ufeff \\ufffd"'],
  ['unicode raw', '"caf\u00e9 \u65e5\u672c \u{1f600}"'],
  ['escaped slash', '"\\/"'],
  ['zero', '0'],
  ['negative zero', '-0'],
  ['negative zero float', '-0.0'],
  ['one', '1'],
  ['negative', '-1'],
  ['fraction', '0.1'],
  ['half', '0.5'],
  ['float sum', '0.30000000000000004'],
  ['4.35', '4.35'],
  ['third', '0.3333333333333333'],
  ['exponent upper', '1E2'],
  ['exponent plus', '1e+2'],
  ['trailing zeros', '1.50000'],
  ['1e20', '1e20'],
  ['1e21', '1e21'],
  ['123e20', '123e20'],
  ['1.5e300', '1.5e300'],
  ['max float', '1.7976931348623157e308'],
  ['min subnormal', '5e-324'],
  ['min normal', '2.2250738585072014e-308'],
  ['1e-6', '1e-6'],
  ['0.000001', '0.000001'],
  ['1e-7', '1e-7'],
  ['1.234e-7', '1.234e-7'],
  ['0.000001234', '0.000001234'],
  ['-1.5e-10', '-1.5e-10'],
  ['2^53', '9007199254740992'],
  ['2^53+1', '9007199254740993'],
  ['big integer', '123456789012345678901'],
  ['bigger integer', '12345678901234567890123456789'],
  ['999999999999999999999', '999999999999999999999'],
  ['100000000000000000000', '100000000000000000000'],
  ['1e-400 underflow', '1e-400'],
  ['mixed object', '{"total":{"$gte":100},"currency":"USD","tags":["a","b"],"flag":true,"none":null}'],
  ['deep', '{"a":{"b":{"c":{"d":{"e":{"f":[1,{"g":"h"}]}}}}}}'],
  ['whitespace', ' { "b" : [ 1 , 2 ] ,\n\t"a" : "x" } '],
];

const canonical = canonicalInputs.map(([name, input]) => ({ name, input, canonical: canonicalJson(JSON.parse(input)) }));

// ---- subscription IDs (spec/guide derivation over already-normalized URLs).
const idInputs = [
  ['user_8f2c', 'https://receiver.example.com/mcp-events/abc123', 'order.created', '{"total":{"$gte":100},"currency":"USD"}'],
  ['user_8f2c', 'https://receiver.example.com/mcp-events/abc123', 'order.created', '{"currency":"USD","total":{"$gte":100}}'],
  ['user_8f2c', 'https://receiver.example.com/mcp-events/abc123', 'order.created', '{}'],
  ['user_8f2c', 'https://receiver.example.com/mcp-events/abc123', 'order.created', '{"currency":["EUR","USD"]}'],
  ['user_8f2c', 'https://connectors.api.openai.com/webhook/mcp-events/0123456789abcdef0123456789abcdef', 'incident.created', '{"severity":"P1"}'],
  ['auth0|5f7c8ec7c33c6c004bbafe82', 'https://hooks.example.com/x?y=1', 'order.created', '{"total":1e21,"min":5e-324}'],
  ['pr\u2028incipal "quoted" \\ \u00e9 \u{1f600}', 'https://xn--bcher-kva.de/p%C3%A9', 'topic.\u00e9', '{"k\u2029":"v\\u0001","n":-0}'],
  ['', 'https://a.example/', '', '{}'],
  ['tenant\u0000principal', 'http://127.0.0.1:8080/hook', 'a.b', '{"x":[1,2,3],"y":{"$lt":0.1}}'],
];

const ids = idInputs.map(([principal, url, name, args]) => {
  const parsed = JSON.parse(args);
  return {
    principal,
    url,
    name,
    arguments: args,
    canonical_key: canonicalJson({ principal, url, name, arguments: parsed }),
    id: deriveSubscriptionId(principal, url, name, parsed),
  };
});

// ---- WHATWG URL serialization for the callback URL normalization vector set.
const urlInputs = [
  'https://example.com',
  'https://example.com/',
  'HTTPS://EXAMPLE.COM/Path?Q=1',
  'https://Example.COM./a',
  'https://example.com../a',
  'https://example.com:443/',
  'https://example.com:0443/',
  'https://example.com:00443/x',
  'https://example.com:8443/',
  'https://example.com:08443/',
  'https://example.com:65535/',
  'https://example.com:65536/',
  'https://example.com:0/',
  'https://example.com:/',
  'http://example.com:80/',
  'http://example.com:443/',
  'https://example.com:80/',
  'https://b\u00fccher.de/',
  'https://B\u00dcCHER.de/',
  'https://xn--bcher-kva.de/',
  'https://fa\u00df.de/',
  'https://\u4f8b\u3048.\u30c6\u30b9\u30c8/',
  'https://my_host.example.com/',
  'https://-lead.example.com/',
  'https://r3---sn-abc.example.com/',
  'https://127.0.0.1/',
  'https://127.0.0.1.:8443/',
  'https://127.1/',
  'https://0x7f.0.0.1/',
  'https://2130706433/',
  'https://127.000.0.1/',
  'https://1.2.3.256/',
  'https://example.123/',
  'https://[::1]/',
  'https://[::1]:443/',
  'https://[0:0:0:0:0:0:0:1]:8443/',
  'https://[2001:DB8:0:0:0:0:0:1]/',
  'https://[2001:4860:4860:0:0:0:0:8888]/',
  'https://[::ffff:7f00:1]/',
  'https://[::ffff:127.0.0.1]/',
  'https://[0:0:0:0:0:ffff:127.0.0.1]/',
  'https://[::127.0.0.1]/',
  'https://[fe80::1%25en0]/',
  'https://user:pass@example.com/',
  'https://user@example.com/',
  'https://@example.com/',
  'https://example.com/#frag',
  'https://example.com/#',
  'https://example.com/?',
  'https://example.com/?a=1&b=2',
  'https://example.com/?q=<x>"y"\'z\'`{}|^',
  'https://example.com/a b',
  'https://example.com/a<b>"c"`d{e}f^g|h',
  'https://example.com/caf\u00e9/\u65e5',
  'https://example.com/%7e%7E%zz',
  'https://example.com/%2e/a/./b/../c',
  'https://example.com/a/%2E%2e/b',
  'https://example.com/a/b/..',
  'https://example.com/a/b/.',
  'https://example.com/..',
  'https://example.com//double//slash/',
  'https://example.com/a!$&\'()*+,;=:@[]~-._',
  'https://example.com/a\\b',
  'https://example.com\\@evil.com/',
  ' https://example.com/ ',
  'https://exa\tmple.com/',
  'https:example.com/',
  'https:/example.com/',
  '//example.com/',
  '/relative/path',
  'example.com',
  'ftp://example.com/',
  'javascript:alert(1)',
  'file:///etc/passwd',
  'wss://example.com/',
  'https://',
  'https:///path',
  'https://:443/',
  'https://ex%61mple.com/',
  'https://example.com/\u2028',
];

const urls = urlInputs.map((input) => {
  try {
    return { input, href: new URL(input).href };
  } catch (error) {
    return { input, error: String(error.code ?? error.message) };
  }
});

const write = (file, value) => writeFileSync(join(outDir, file), JSON.stringify(value, null, 2) + '\n');
write('canonical_json.json', canonical);
write('subscription_ids.json', ids);
write('whatwg_urls.json', urls);
console.log(`wrote ${canonical.length} canonical, ${ids.length} id and ${urls.length} url vectors to ${outDir}`);
