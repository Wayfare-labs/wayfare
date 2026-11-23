"""Minimal, stdlib-only Soroban RPC client for read-only research.

Builds just enough XDR to (a) simulate a contract call and (b) read ledger
entries, and decodes SCVal results into plain Python values. Nothing is
signed and nothing is submitted: simulateTransaction never touches the ledger.
"""
import base64, json, os, struct, sys, urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from sac import strkey_decode, strkey_encode

RPC = "https://mainnet.sorobanrpc.com"
PASSPHRASE = "Public Global Stellar Network ; September 2015"
# Any existing account works as the simulation source; this is Circle's USDC
# issuer, chosen only because it is certain to exist.
SOURCE = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"


def rpc(method, params, url=RPC):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(url, body, {"Content-Type": "application/json", "User-Agent": "research-script"})
    with urllib.request.urlopen(req, timeout=60) as r:
        d = json.load(r)
    if "error" in d:
        raise RuntimeError(d["error"])
    return d["result"]


# ---------- XDR encoding ----------
def u32(n): return struct.pack(">I", n)
def i32(n): return struct.pack(">i", n)
def i64(n): return struct.pack(">q", n)
def opaque_var(b): return u32(len(b)) + b + b"\0" * ((4 - len(b) % 4) % 4)


class Sym(str):
    """Marks a str as an SCV_SYMBOL rather than an SCV_STRING."""


class I128(int):
    pass


class U32(int):
    pass


class U128(int):
    pass


def sc_address(s):
    if s.startswith("G"):
        return i32(0) + i32(0) + strkey_decode(s)
    if s.startswith("C"):
        return i32(1) + strkey_decode(s)
    raise ValueError(s)


def scval(v):
    if isinstance(v, bool):
        return i32(0) + u32(1 if v else 0)
    if v is None:
        return i32(1)
    if isinstance(v, U32):
        return i32(3) + u32(v)
    if isinstance(v, U128):
        return i32(9) + struct.pack(">QQ", v >> 64, v & ((1 << 64) - 1))
    if isinstance(v, I128):
        hi, lo = v >> 64, v & ((1 << 64) - 1)
        return i32(10) + i64(hi) + struct.pack(">Q", lo)
    if isinstance(v, int):
        return i32(6) + i64(v)
    if isinstance(v, Sym):
        return i32(15) + opaque_var(v.encode())
    if isinstance(v, str) and len(v) == 56 and v[0] in "GC":
        return i32(18) + sc_address(v)
    if isinstance(v, str):
        return i32(14) + opaque_var(v.encode())
    if isinstance(v, list):
        return i32(16) + u32(1) + u32(len(v)) + b"".join(scval(x) for x in v)
    if isinstance(v, dict):  # keys are symbols, sorted as Soroban requires
        items = sorted(v.items())
        return i32(17) + u32(1) + u32(len(items)) + b"".join(scval(Sym(k)) + scval(x) for k, x in items)
    raise TypeError(type(v))


def invoke_tx(contract, fn, args):
    op_body = (i32(24)  # INVOKE_HOST_FUNCTION
               + i32(0)  # HOST_FUNCTION_TYPE_INVOKE_CONTRACT
               + sc_address(contract) + opaque_var(fn.encode())
               + u32(len(args)) + b"".join(scval(a) for a in args)
               + u32(0))  # no auth entries
    tx = (i32(0) + strkey_decode(SOURCE)  # MuxedAccount ed25519
          + u32(100) + i64(0) + i32(0) + i32(0)  # fee, seq, PRECOND_NONE, MEMO_NONE
          + u32(1) + u32(0) + op_body  # one op, no op source
          + i32(0))  # ext
    return base64.b64encode(i32(2) + tx + u32(0)).decode()  # ENVELOPE_TYPE_TX, no sigs


def contract_data_key(contract, key_scval_bytes, durability=1):
    return base64.b64encode(i32(6) + sc_address(contract) + key_scval_bytes + i32(durability)).decode()


def instance_key(contract):
    return contract_data_key(contract, i32(20))  # SCV_LEDGER_KEY_CONTRACT_INSTANCE


# ---------- XDR decoding ----------
class R:
    def __init__(self, b): self.b, self.p = b, 0
    def take(self, n):
        out = self.b[self.p:self.p + n]; self.p += n; return out
    def u32(self): return struct.unpack(">I", self.take(4))[0]
    def i32(self): return struct.unpack(">i", self.take(4))[0]
    def u64(self): return struct.unpack(">Q", self.take(8))[0]
    def i64(self): return struct.unpack(">q", self.take(8))[0]
    def var(self):
        n = self.u32(); out = self.take(n); self.take((4 - n % 4) % 4); return out


def read_address(r):
    t = r.i32()
    if t == 0:
        r.i32(); return strkey_encode(6 << 3, r.take(32))
    if t == 1:
        return strkey_encode(2 << 3, r.take(32))
    raise ValueError("address type %d" % t)


def read_scval(r):
    t = r.i32()
    if t == 0: return bool(r.u32())
    if t == 1: return None
    if t == 2:
        return {"error": (r.i32(), r.u32())}
    if t in (3,): return r.u32()
    if t in (4,): return r.i32()
    if t in (5, 7, 8): return r.u64()
    if t == 6: return r.i64()
    if t == 9:
        hi, lo = r.u64(), r.u64(); return (hi << 64) | lo
    if t == 10:
        hi, lo = r.i64(), r.u64(); return (hi << 64) | lo
    if t in (11, 12):
        parts = [r.u64() for _ in range(4)]; v = 0
        for p in parts: v = (v << 64) | p
        return v
    if t == 13: return r.var().hex()
    if t in (14, 15): return r.var().decode()
    if t == 16:
        if not r.u32(): return None
        return [read_scval(r) for _ in range(r.u32())]
    if t == 17:
        if not r.u32(): return None
        out = []
        for _ in range(r.u32()):
            k = read_scval(r); out.append((k, read_scval(r)))
        return out
    if t == 18: return read_address(r)
    if t == 19:
        et = r.i32()
        exe = r.take(32).hex() if et == 0 else "stellar-asset"
        storage = None
        if r.u32():
            storage = []
            for _ in range(r.u32()):
                k = read_scval(r); storage.append((k, read_scval(r)))
        return {"executable": exe, "storage": storage}
    if t == 20: return "LEDGER_KEY_CONTRACT_INSTANCE"
    raise ValueError("scval type %d" % t)


def decode_scval(b64):
    return read_scval(R(base64.b64decode(b64)))


def decode_contract_data_entry(b64):
    """LedgerEntryData for CONTRACT_DATA -> (contract, key, value)."""
    r = R(base64.b64decode(b64))
    assert r.i32() == 6
    r.i32()  # ext
    contract = read_address(r)
    key = read_scval(r)
    r.i32()  # durability
    return contract, key, read_scval(r)


# ---------- high-level helpers ----------
def simulate(contract, fn, *args):
    """Returns (decoded value or None, raw response, latestLedger)."""
    res = rpc("simulateTransaction", {"transaction": invoke_tx(contract, fn, list(args))})
    if res.get("error"):
        return None, res, res.get("latestLedger")
    return decode_scval(res["results"][0]["xdr"]), res, res.get("latestLedger")


def get_entries(keys):
    return rpc("getLedgerEntries", {"keys": keys})


def instance_storage(contract):
    res = get_entries([instance_key(contract)])
    if not res.get("entries"):
        return None, res
    _, _, inst = decode_contract_data_entry(res["entries"][0]["xdr"])
    return inst, res


def jsonable(v):
    if isinstance(v, list):
        return [jsonable(x) for x in v]
    if isinstance(v, tuple):
        return [jsonable(x) for x in v]
    if isinstance(v, dict):
        return {k: jsonable(x) for k, x in v.items()}
    if isinstance(v, int) and not isinstance(v, bool) and abs(v) > 2**53:
        return str(v)
    return v
