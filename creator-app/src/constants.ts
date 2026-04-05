export const INITIAL_PORT_BASE = 10000;

export const SCAN_INTERVAL_MS = 2000;
export const KICK_DELAY_MS = 500;
export const RELAY_RESTART_DELAY_MS = 500;
export const HOOK_POLL_INTERVAL_MS = 500;
export const CALL_CREATOR_INJECT_DELAY_MS = 1000;
export const BOT_POLL_RETRY_DELAY_MS = 1000;
export const BOT_POLL_WAIT_SECONDS = 25;

export const VK_API_VERSION = '5.131';
export const VK_API_BASE_URL = 'https://api.vk.com/method';
export const VK_IM_URL = 'https://vk.com/im';
export const TELEMOST_URL = 'https://telemost.yandex.ru/';

export const SESSION_PARTITION = 'persist:creator';
export const WINDOW_WIDTH = 1200;
export const WINDOW_HEIGHT = 800;

export const USER_AGENT =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ' +
  'AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

export const VK_COOKIE_DOMAINS = ['vk.com', 'vk.ru'];
export const YANDEX_COOKIE_DOMAINS = ['yandex.ru', 'yandex.net', 'ya.ru'];

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
  GET_COOKIES = 'get-cookies',
  RELAY_LOG = 'relay-log',
  CREATE_BOT_TAB = 'create-bot-tab',
  CLOSE_BOT_TAB = 'close-bot-tab',
  BOT_ERROR = 'bot-error',
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
  SHORT_LINK = 'short:',
  TURN = 'TURN:',
  PROTOCOL = 'protocol:',
  TUNNEL_CONNECTED = 'TUNNEL CONNECTED',
}
