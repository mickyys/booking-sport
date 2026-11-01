/**
 * Action de Auth0 (trigger: Login / Post Login).
 *
 * Copia los roles del usuario al ID token y al ACCESS token, para que el
 * backend pueda autorizar /api/superadmin/* sin llamar a la Management API.
 *
 * El namespace debe coincidir con NEXT_PUBLIC_AUTHO_ROL_CLAIM del frontend.
 * Configúralo como Secret en la Action: ROLES_CLAIM
 * (por defecto: https://myapp.com/roles).
 */
exports.onExecutePostLogin = async (event, api) => {
  const ns = (event.secrets && event.secrets.ROLES_CLAIM) || 'https://myapp.com/roles';
  const roles = (event.authorization && event.authorization.roles) || [];

  api.idToken.setCustomClaim(ns, roles);
  // Esta es la línea que faltaba: el backend valida el access token.
  api.accessToken.setCustomClaim(ns, roles);
};
