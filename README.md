# booking-sport

API REST en Go (Gin + MongoDB + Redis). Ver `Makefile` y `docker-compose.yml` para el entorno local.

## Mercado Pago Connect (OAuth de vendedores)

Cada ambiente (local/ngrok, dev, prod) debe usar **su propia aplicación** de Mercado Pago, o la misma aplicación con todas las URLs registradas.

### Checklist al configurar un ambiente nuevo (ej. producción)

1. En Mercado Pago → Tus integraciones → la aplicación → **URLs de redireccionamiento**, agregar la URL exacta del callback:

   | Ambiente | Redirect URI |
   |---|---|
   | Local (ngrok) | `https://<subdominio>.ngrok-free.app/api/admin/mercadopago/callback` |
   | Dev | `https://api.dev.reservaloya.cl/api/admin/mercadopago/callback` |
   | Prod | `https://<api-prod>/api/admin/mercadopago/callback` |

   Debe coincidir carácter por carácter con `MP_REDIRECT_URI` (sin `/` final, mismo esquema y dominio). Si falta, Mercado Pago rechaza la autorización en su propia pantalla, antes de llamar al backend.

2. Confirmar que la aplicación tiene habilitado Connect/Marketplace (OAuth) y es del país correcto (Chile).
3. Configurar las variables del backend (todas de la **misma** aplicación):

   | Variable | Valor |
   |---|---|
   | `NEXT_PUBLIC_MP_CLIENT_ID` | Client ID de la app (lo lee el backend, no la UI) |
   | `MP_CLIENT_SECRET` | Client secret de la app. Además firma el `state` de OAuth |
   | `MP_REDIRECT_URI` | La redirect URI registrada en el paso 1 |
   | `MP_AUTH_BASE` | `https://auth.mercadopago.cl` |
   | `NEXT_PUBLIC_MP_PUBLIC_KEY` | Public key de la app |
   | `MP_ACCESS_TOKEN` | Access token de la plataforma (misma app) |
   | `MP_WEBHOOK_SECRET` | Secret del webhook de ese ambiente |
   | `URL_MP_WEBHOOK`, `URL_MP_CALLBACK` | URLs del ambiente, no de otro |
   | `URL_FRONTEND` | URL del front del ambiente (destino tras el callback) |

4. Redeployar el servicio tras cambiar variables.

### Diagnóstico

- Error en la pantalla de Mercado Pago: casi siempre `redirect_uri` no registrada, `client_id` de otra app o Connect deshabilitado.
- Redirección a `/admin/onboarding?status=error&reason=callback_failed`: buscar `mp_connect_callback_error` en los logs (usar el `trace_id` de la URL). Suele ser `MP_CLIENT_SECRET` incorrecto o `MP_REDIRECT_URI` distinto del usado al autorizar.
- `reason=missing_code`: el usuario canceló o Mercado Pago devolvió un error (`mp_error` en el log `mp_connect_callback_received`).
