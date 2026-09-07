import { Platform, TunnelMode } from '../types';
import {
  VK_COOKIE_DOMAINS,
  YANDEX_COOKIE_DOMAINS,
  DION_COOKIE_DOMAINS,
  WBSTREAM_COOKIE_DOMAINS,
  VK_LOGIN_URL,
  YANDEX_LOGIN_URL,
  DION_LOGIN_URL,
  WBSTREAM_LOGIN_URL,
  VK_AUTH_COOKIE,
  YANDEX_AUTH_COOKIE,
  DION_AUTH_COOKIE,
  WBSTREAM_AUTH_COOKIE,
} from '../constants';

export interface PlatformConfig {
  tunnelMode: TunnelMode;
  authCookie: string;
  refreshCookie: string;
  loginUrl: string;
  cookieDomains: string[];
  platformName: string;
  binarySubdir: string;
  binaryBase: string;
  joinFlag: string | null;
  loginWaitCookies: string[] | null;
}

export const PLATFORM_CONFIG: Partial<Record<Platform, PlatformConfig>> = {
  [Platform.VK]: {
    tunnelMode: TunnelMode.HeadlessVK,
    authCookie: VK_AUTH_COOKIE,
    refreshCookie: VK_AUTH_COOKIE,
    loginUrl: VK_LOGIN_URL,
    cookieDomains: VK_COOKIE_DOMAINS,
    platformName: 'VK',
    binarySubdir: 'vk',
    binaryBase: 'headless-vk-creator',
    joinFlag: '--vk-link',
    loginWaitCookies: null,
  },
  [Platform.Telemost]: {
    tunnelMode: TunnelMode.HeadlessTelemost,
    authCookie: YANDEX_AUTH_COOKIE,
    refreshCookie: YANDEX_AUTH_COOKIE,
    loginUrl: YANDEX_LOGIN_URL,
    cookieDomains: YANDEX_COOKIE_DOMAINS,
    platformName: 'Yandex',
    binarySubdir: 'telemost',
    binaryBase: 'headless-telemost-creator',
    joinFlag: '--tm-link',
    loginWaitCookies: null,
  },
  [Platform.Dion]: {
    tunnelMode: TunnelMode.HeadlessDion,
    authCookie: DION_AUTH_COOKIE,
    refreshCookie: DION_AUTH_COOKIE,
    loginUrl: DION_LOGIN_URL,
    cookieDomains: DION_COOKIE_DOMAINS,
    platformName: 'DION',
    binarySubdir: 'dion',
    binaryBase: 'headless-dion-creator',
    joinFlag: '--room',
    loginWaitCookies: ['vc-refresh-token', 'vc-access-token'],
  },
  [Platform.WBStream]: {
    tunnelMode: TunnelMode.HeadlessWBStream,
    authCookie: WBSTREAM_AUTH_COOKIE,
    refreshCookie: 'wbx-refresh',
    loginUrl: WBSTREAM_LOGIN_URL,
    cookieDomains: WBSTREAM_COOKIE_DOMAINS,
    platformName: 'WB Stream',
    binarySubdir: 'wbstream',
    binaryBase: 'headless-wbstream-creator',
    joinFlag: '--room',
    loginWaitCookies: ['x_wbaas_token', 'wbx-refresh', 'wbx-validation-key'],
  },
};
