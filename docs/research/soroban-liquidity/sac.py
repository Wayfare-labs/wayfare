"""Stellar Asset Contract (SAC) IDs, derived offline from (code, issuer).

Checked against the two published IDs everyone knows: native XLM is
CAS3J7GY...OWMA and Circle USDC is CCW67TSZ...MI75 (see capture output).
"""
import hashlib, base64, struct, sys
ALPH = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
def crc16(data):
    crc = 0
    for b in data:
        crc ^= b << 8
        for _ in range(8):
            crc = ((crc << 1) ^ 0x1021) if crc & 0x8000 else crc << 1
            crc &= 0xFFFF
    return crc
def strkey_decode(s):
    raw = base64.b32decode(s); return raw[1:-2]
def strkey_encode(ver, payload):
    d = bytes([ver]) + payload
    return base64.b32encode(d + struct.pack("<H", crc16(d))).decode()
def sac(code, issuer, passphrase="Public Global Stellar Network ; September 2015"):
    nid = hashlib.sha256(passphrase.encode()).digest()
    if code == "XLM" and not issuer:
        a = struct.pack(">i", 0)
    else:
        if len(code) <= 4: a = struct.pack(">i", 1) + code.encode().ljust(4, b"\0")
        else: a = struct.pack(">i", 2) + code.encode().ljust(12, b"\0")
        a += struct.pack(">i", 0) + strkey_decode(issuer)
    pre = struct.pack(">i", 8) + nid + struct.pack(">i", 1) + a
    return strkey_encode(2 << 3, hashlib.sha256(pre).digest())
if __name__ == "__main__":
    for arg in sys.argv[1:]:
        code, _, iss = arg.partition(":"); print(arg, sac(code, iss))
