"""Break-glass guard (ADR 0013, docs/break-glass.md): Traefik ForwardAuth for Kargo's Freight approve endpoint.

Kargo checks the same `promote` verb for promoting and for approving Freight that skipped upstream
verification, so RBAC can't split them. Traefik sends only approve requests here; we allow them for
the Stage's `example.com/break-glass` emails (rendered from owners.yml breakGlass) and admins.

The token signature is not checked: kargo-api verifies the token after we allow, so a forged token
never reaches the approval. One JSON log line per decision.
"""
import base64
import json
import os
import re
import ssl
import sys
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

APPROVE = re.compile(r"^/v1beta1/projects/([^/]+)/freight/([^/]+)/approve$")
ADMINS = {e.strip() for e in os.environ.get("ADMIN_EMAILS", "").split(",") if e.strip()}
ANNOTATION = "example.com/break-glass"
API = "https://kubernetes.default.svc"
SA = "/var/run/secrets/kubernetes.io/serviceaccount"


def email_of(auth):
    if not auth.startswith("Bearer "):
        return None
    try:
        payload = auth[len("Bearer "):].split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload)).get("email")
    except (IndexError, ValueError):
        return None


def break_glass(project, stage):
    with open(f"{SA}/token") as f:
        token = f.read()
    q = urllib.parse.quote
    req = urllib.request.Request(
        f"{API}/apis/kargo.akuity.io/v1alpha1/namespaces/{q(project)}/stages/{q(stage)}",
        headers={"Authorization": f"Bearer {token}"},
    )
    ctx = ssl.create_default_context(cafile=f"{SA}/ca.crt")
    with urllib.request.urlopen(req, context=ctx, timeout=5) as r:
        ann = json.load(r)["metadata"].get("annotations", {}).get(ANNOTATION, "[]")
    return set(json.loads(ann))


def decide(headers):
    uri = urllib.parse.urlsplit(headers.get("X-Forwarded-Uri", ""))
    email = email_of(headers.get("Authorization", ""))
    m = APPROVE.match(uri.path)
    stage = urllib.parse.parse_qs(uri.query).get("stage", [None])[0]
    rec = {"email": email, "uri": uri.path, "project": m and m[1], "freight": m and m[2], "stage": stage}
    if email and email in ADMINS:
        return True, "admin", rec
    if not email:
        return False, "no email in token", rec
    if not m or not stage:
        return False, "unsupported approve call (only admins)", rec
    try:
        allowed = break_glass(m[1], stage)
    except Exception as e:  # unknown stage, API error: fail closed
        return False, f"stage lookup failed: {e}", rec
    if email in allowed:
        return True, "break-glass", rec
    return False, "not break-glass for this stage", rec


class Handler(BaseHTTPRequestHandler):
    def handle_any(self):
        if self.path == "/healthz":
            return self.reply(200, {"ok": True})
        ok, reason, rec = decide(self.headers)
        print(json.dumps({"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                          "msg": "break-glass approve", "decision": "allow" if ok else "deny",
                          "reason": reason, **rec}), flush=True)
        if ok:
            return self.reply(200, {"ok": True})
        self.reply(403, {"error": "approving Freight that skipped verification is break-glass only "
                                  f"(owners.yml breakGlass): {reason}"})

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = do_HEAD = handle_any

    def reply(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):  # decisions are logged as JSON above
        pass


if __name__ == "__main__":
    print(json.dumps({"msg": "break-glass guard listening", "admins": sorted(ADMINS)}), flush=True)
    ThreadingHTTPServer(("", 8080), Handler).serve_forever()
    sys.exit(1)
