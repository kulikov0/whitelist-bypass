export const INITIAL_PORT_BASE = 10000;

export const SCAN_INTERVAL_MS = 2000;
export const KICK_DELAY_MS = 500;
export const RELAY_RESTART_DELAY_MS = 500;
export const HOOK_POLL_INTERVAL_MS = 500;
export const CALL_CREATOR_INJECT_DELAY_MS = 1000;
export const BOT_POLL_RETRY_DELAY_MS = 1000;
export const BOT_POLL_WAIT_SECONDS = 25;

export const VK_API_VERSION = '5.131';
export const VK_API_BASE_URL = 'https://api.vk.ru/method';
export const VK_IM_URL = 'https://vk.ru/im';
export const TELEMOST_URL = 'https://telemost.yandex.ru/';
export const DION_URL = 'https://dion.vc/';

export const VK_LOGIN_URL = 'https://vk.ru/';
export const YANDEX_LOGIN_URL = 'https://passport.yandex.ru/auth?retpath=https%3A%2F%2Ftelemost.yandex.ru%2F';
export const DION_LOGIN_URL = 'https://dion.vc/login';
export const WBSTREAM_LOGIN_URL = 'https://stream.wb.ru/login';
export const BITRIX_LOGIN_URL = 'https://www.bitrix24.ru/';
export const VK_AUTH_COOKIE = 'remixsid';
export const YANDEX_AUTH_COOKIE = 'Session_id';
export const DION_AUTH_COOKIE = 'vc-refresh-token';
export const WBSTREAM_AUTH_COOKIE = 'x_wbaas_token';
export const BITRIX_SESSION_COOKIE = 'BITRIX_SM_UIDH';
export const BITRIX_AUTH_NET_HOST = 'https://auth2.bitrix24.net';

export const SESSION_PARTITION = 'persist:creator';
export const WINDOW_WIDTH = 1200;
export const WINDOW_HEIGHT = 800;

export const USER_AGENT =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) ' +
  'AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36';

export const VK_COOKIE_DOMAINS = ['vk.ru'];
export const YANDEX_COOKIE_DOMAINS = ['yandex.ru', 'yandex.net', 'ya.ru'];
export const DION_COOKIE_DOMAINS = ['dion.vc'];
export const WBSTREAM_COOKIE_DOMAINS = ['stream.wb.ru', 'wb.ru', 'wildberries.ru'];
export const BITRIX_COOKIE_DOMAINS = ['bitrix24.ru', 'bitrix24.net', 'bitrix24.com'];

export enum Selector {
  VK_ADMIT = '[data-testid="calls_waiting_hall_promote"]',
  VK_PARTICIPANT_MENU = '[data-testid="calls_participant_list_item_menu_button"]',
  VK_KICK = '[data-testid="calls_participant_actions_kick"]',
  VK_KICK_CONFIRM = '[data-testid="calls_call_kick_submit"]',
  VK_LEAVE_CALL = '[data-testid="calls_call_footer_button_leave_call"]',
  VK_CALL_MENU_TRIGGER = 'call-menu-trigger',
  TM_END_CALL = '[data-testid="end-call-alt-button"]',
  TM_CREATE_CALL = '[data-testid="create-call-button"]',
  TM_MODERATION_POPUP = '[data-testid="show-moderation-popup"]',
  TM_MODAL = '[data-testid="orb-modal2"]',
}

export enum IPC {
  GET_HOOK_CODE = 'get-hook-code',
  GET_CALL_CREATOR_CODE = 'get-call-creator-code',
  SET_TUNNEL_MODE = 'set-tunnel-mode',
  START_RELAY = 'start-relay',
  START_HEADLESS = 'start-headless',
  CLOSE_TAB = 'close-tab',
  START_BOT = 'start-bot',
  STOP_BOT = 'stop-bot',
  SET_UPSTREAM_PROXY = 'set-upstream-proxy',
  SET_DEBUG_LOGGING = 'set-debug-logging',
  CLEAR_COOKIES = 'clear-cookies',
  GET_DION_CREDENTIALS = 'get-dion-credentials',
  SET_DION_CREDENTIALS = 'set-dion-credentials',
  GET_BITRIX_CREDENTIALS = 'get-bitrix-credentials',
  SET_BITRIX_CREDENTIALS = 'set-bitrix-credentials',
  EXPORT_COOKIES_ZIP = 'export-cookies-zip',
  RENDER_QR = 'render-qr',
  RELAY_LOG = 'relay-log',
  CREATE_BOT_TAB = 'create-bot-tab',
  CLOSE_BOT_TAB = 'close-bot-tab',
  BOT_ERROR = 'bot-error',
  SEND_BOT_CALL_LINK = 'send-bot-call-link',
  LOGIN_REQUIRED = 'login-required',
  LOGIN_DONE = 'login-done',
}

export const LOG_CAPTURE_SNIPPET = [
  'if(!window.__logCaptureInstalled){',
  'window.__logCaptureInstalled=true;',
  'window.__hookLogs=[];',
  'var _ol=console.log.bind(console);',
  'console.log=function(){',
  '_ol.apply(null,arguments);',
  "var m=Array.prototype.slice.call(arguments).join(' ');",
  "if(m.indexOf('[HOOK]')!==-1)window.__hookLogs.push(m)",
  '}}',
].join('');

export enum HeadlessLogMarker {
  CALL_CREATED = 'CALL CREATED',
  JOIN_LINK = 'join_link:',
  TURN = 'TURN:',
  PROTOCOL = 'protocol:',
  TUNNEL_CONNECTED = 'TUNNEL CONNECTED',
}
