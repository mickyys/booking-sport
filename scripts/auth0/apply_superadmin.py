#!/usr/bin/env python3
"""Aplica en Auth0 la configuración del panel superadmin de ReservaloYA.

Hace tres cosas, de forma idempotente:
  1. Crea o actualiza la Action de Post Login que copia los roles del usuario
     al ID token y al ACCESS token (el backend valida el access token).
  2. Agrega esa Action al flow de Login si no está.
  3. Verifica que la app M2M pueda leer roles (scope read:roles), usado como
     respaldo por el backend cuando el token no trae el claim.

Credenciales: se leen de booking-sport/.env
  AUTH0_MGMT_DOMAIN, AUTH0_CLIENT_ID_MACHINE, AUTH0_CLIENT_SECRET_MACHINE

La app M2M necesita estos scopes en la Auth0 Management API:
  read:actions create:actions update:actions read:flows update:flows

Uso:
  ./scripts/auth0/apply_superadmin.py           # aplica
  ./scripts/auth0/apply_superadmin.py --check   # solo muestra el estado
"""

import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ACTION_NAME = "ReservaloYA - roles en access token"
ACTION_FILE = Path(__file__).resolve().parent / "post-login-roles.js"
SCOPES = "read:actions create:actions update:actions read:flows update:flows"
CLAIM_KEY = "ROLES_CLAIM"


def load_env() -> None:
    env_file = ROOT / ".env"
    if not env_file.exists():
        return
    for line in env_file.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        os.environ.setdefault(key.strip(), value.strip().strip('"').strip("'"))


def http(method: str, url: str, token: str | None = None, body: dict | None = None) -> tuple[int, dict | list | str]:
    cmd = ["curl", "-s", "-w", "\n%{http_code}", "-X", method, url]
    if token:
        cmd += ["-H", f"Authorization: Bearer {token}"]
    if body is not None:
        cmd += ["-H", "content-type: application/json", "-d", json.dumps(body)]
    out = subprocess.run(cmd, capture_output=True, text=True, check=False).stdout
    raw, _, status = out.rpartition("\n")
    try:
        parsed = json.loads(raw) if raw else {}
    except json.JSONDecodeError:
        parsed = raw
    return int(status or 0), parsed


def get_token(domain: str, client_id: str, secret: str) -> tuple[str, str]:
    status, body = http(
        "POST",
        f"https://{domain}/oauth/token",
        body={
            "grant_type": "client_credentials",
            "client_id": client_id,
            "client_secret": secret,
            "audience": f"https://{domain}/api/v2/",
            "scope": SCOPES,
        },
    )
    if status != 200 or not isinstance(body, dict) or "access_token" not in body:
        detail = body.get("error_description") if isinstance(body, dict) else body
        return "", str(detail or f"HTTP {status}")
    return body["access_token"], ""


def find_action(domain: str, token: str) -> str | None:
    status, body = http("GET", f"https://{domain}/api/v2/actions/actions?per_page=100", token)
    if status != 200 or not isinstance(body, dict):
        raise RuntimeError(f"no se pudieron listar actions (HTTP {status}): {body}")
    for action in body.get("actions", []):
        if action.get("name") == ACTION_NAME:
            return action["id"]
    return None


def login_flow_id(domain: str, token: str) -> str:
    status, body = http("GET", f"https://{domain}/api/v2/flows?per_page=50", token)
    if status == 200 and isinstance(body, dict):
        for flow in body.get("flows", []):
            if flow.get("name") == "Login":
                return flow["id"]
    return "af_login"


def action_in_flow(domain: str, token: str, flow_id: str, action_id: str) -> bool:
    status, body = http(
        "GET", f"https://{domain}/api/v2/flows/{flow_id}/executions?per_page=50", token
    )
    if status != 200 or not isinstance(body, dict):
        return False
    return any(
        (e.get("action") or {}).get("id") == action_id for e in body.get("executions", [])
    )


def m2m_roles_readable(domain: str, token: str) -> bool:
    """Comprueba el scope read:roles leyendo los roles del primer usuario."""
    status, body = http("GET", f"https://{domain}/api/v2/users?per_page=1&fields=user_id", token)
    if status != 200 or not isinstance(body, list) or not body:
        return False
    user_id = body[0].get("user_id", "")
    status, _ = http("GET", f"https://{domain}/api/v2/users/{user_id.replace('|', '%7C')}/roles", token)
    return status == 200


def main() -> int:
    load_env()
    domain = os.environ.get("AUTH0_MGMT_DOMAIN") or os.environ.get("AUTH0_DOMAIN", "")
    client_id = os.environ.get("AUTH0_CLIENT_ID_MACHINE", "")
    secret = os.environ.get("AUTH0_CLIENT_SECRET_MACHINE", "")
    if not (domain and client_id and secret):
        print("Faltan AUTH0_MGMT_DOMAIN / AUTH0_CLIENT_ID_MACHINE / AUTH0_CLIENT_SECRET_MACHINE", file=sys.stderr)
        return 1

    token, error = get_token(domain, client_id, secret)
    if not token:
        print(f"No se pudo obtener token de la Management API: {error}", file=sys.stderr)
        print(
            "\nHabilita en Auth0 Dashboard > Applications > (app Machine to Machine)\n"
            "> APIs > Auth0 Management API > Authorized Scopes:\n"
            f"  {SCOPES}\n"
            "y vuelve a ejecutar este script.",
            file=sys.stderr,
        )
        return 2

    check_only = "--check" in sys.argv
    action_id = find_action(domain, token)

    if check_only:
        print(f"Action «{ACTION_NAME}»: {'existe ' + action_id if action_id else 'no existe'}")
        print(f"Lectura de roles vía M2M (read:roles): {'sí' if m2m_roles_readable(domain, token) else 'no'}")
        return 0

    code = ACTION_FILE.read_text()
    claim = os.environ.get("NEXT_PUBLIC_AUTHO_ROL_CLAIM", "https://myapp.com/roles")

    if action_id:
        print(f"Actualizando action existente ({action_id})…")
        status, body = http("PATCH", f"https://{domain}/api/v2/actions/actions/{action_id}", token, {"code": code})
    else:
        print(f"Creando action «{ACTION_NAME}»…")
        status, body = http(
            "POST",
            f"https://{domain}/api/v2/actions/actions",
            token,
            {
                "name": ACTION_NAME,
                "supported_triggers": [{"id": "post-login", "version": "v3"}],
                "code": code,
                "secrets": [{"name": CLAIM_KEY, "value": claim}],
            },
        )
        if status in (200, 201) and isinstance(body, dict):
            action_id = body.get("id")

    if status not in (200, 201) or not action_id:
        print(f"No se pudo crear/actualizar la action (HTTP {status}): {body}", file=sys.stderr)
        return 3

    status, body = http("POST", f"https://{domain}/api/v2/actions/actions/{action_id}/deploy", token, {})
    if status not in (200, 201):
        print(f"No se pudo desplegar la action (HTTP {status}): {body}", file=sys.stderr)
        return 4
    print("Action desplegada.")

    flow_id = login_flow_id(domain, token)
    if action_in_flow(domain, token, flow_id, action_id):
        print("La action ya estaba en el flow Login.")
    else:
        status, body = http(
            "POST",
            f"https://{domain}/api/v2/flows/{flow_id}/executions",
            token,
            {"action": {"id": action_id}},
        )
        if status not in (200, 201):
            print(f"No se pudo agregar la action al flow Login (HTTP {status}): {body}", file=sys.stderr)
            return 5
        print("Action agregada al flow Login.")

    print(f"Lectura de roles vía M2M (respaldo): {'sí' if m2m_roles_readable(domain, token) else 'no'}")
    print(f"Listo. Cierra sesión y vuelve a entrar para obtener el claim «{claim}» en el access token.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
