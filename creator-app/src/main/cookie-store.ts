import { app, session } from 'electron';
import * as path from 'path';
import * as fs from 'fs/promises';
import { Platform, DionCredentials, BitrixCredentials } from '../types';
import {
  SESSION_PARTITION,
  VK_COOKIE_DOMAINS,
  YANDEX_COOKIE_DOMAINS,
  DION_COOKIE_DOMAINS,
  WBSTREAM_COOKIE_DOMAINS,
  BITRIX_COOKIE_DOMAINS,
  BITRIX_SESSION_COOKIE,
  BITRIX_AUTH_NET_HOST,
} from '../constants';
import { PLATFORM_CONFIG } from './platform-config';
import { DionCookieFile } from './dion-cookie-file';
import { BitrixCookieFile, BitrixCookie } from './bitrix-cookie-file';
import { buildStoredZip } from './util/zip';

export interface BitrixSession {
  portal: string;
  cookies: BitrixCookie[];
}

export class CookieStore {
  cookieFilePath(platform: Platform): string {
    return path.join(app.getPath('userData'), `cookies-${platform}.json`);
  }

  async getCookiesForDomains(domains: string[]): Promise<{ name: string; value: string }[]> {
    const ses = session.fromPartition(SESSION_PARTITION);
    const all = await ses.cookies.get({});
    return all
      .filter((c) => c.domain != null && domains.some((d) => c.domain!.includes(d)))
      .map((c) => ({ name: c.name, value: c.value }));
  }

  waitForLogin(cookieDomains: string[], authCookieName: string): Promise<void> {
    return new Promise((resolve) => {
      const ses = session.fromPartition(SESSION_PARTITION);
      const finish = () => {
        ses.cookies.removeListener('changed', onChanged);
        resolve();
      };
      const onChanged = (
        _e: Electron.Event,
        cookie: Electron.Cookie,
        _cause: string,
        removed: boolean,
      ) => {
        if (removed) return;
        if (cookie.name !== authCookieName) return;
        if (!cookie.domain || !cookieDomains.some((d) => cookie.domain!.includes(d))) return;
        finish();
      };
      ses.cookies.on('changed', onChanged);
      ses.cookies.get({ name: authCookieName }).then((found) => {
        if (found.some((c) => c.domain && cookieDomains.some((d) => c.domain!.includes(d)))) {
          finish();
        }
      });
    });
  }

  waitForCookies(names: string[]): Promise<void> {
    return new Promise((resolve) => {
      const ses = session.fromPartition(SESSION_PARTITION);
      const remaining = new Set(names);
      const finish = () => {
        ses.cookies.removeListener('changed', onChanged);
        resolve();
      };
      const onChanged = (
        _e: Electron.Event,
        cookie: Electron.Cookie,
        _cause: string,
        removed: boolean,
      ) => {
        if (removed) return;
        if (!remaining.has(cookie.name)) return;
        remaining.delete(cookie.name);
        if (remaining.size === 0) finish();
      };
      ses.cookies.on('changed', onChanged);
      Promise.all(names.map((name) => ses.cookies.get({ name }))).then((results) => {
        results.forEach((found, i) => {
          if (found.length > 0) remaining.delete(names[i]);
        });
        if (remaining.size === 0) finish();
      });
    });
  }

  waitForBitrixSession(): Promise<BitrixSession | null> {
    return new Promise((resolve) => {
      const ses = session.fromPartition(SESSION_PARTITION);
      let settled = false;
      const finish = (value: BitrixSession | null): void => {
        if (settled) return;
        settled = true;
        ses.cookies.removeListener('changed', onChanged);
        resolve(value);
      };
      const check = (): void => {
        this.captureBitrixSession().then((data) => {
          if (data) finish(data);
        });
      };
      const onChanged = (
        _e: Electron.Event,
        cookie: Electron.Cookie,
        _cause: string,
        removed: boolean,
      ): void => {
        if (removed) return;
        if (cookie.name !== BITRIX_SESSION_COOKIE) return;
        check();
      };
      ses.cookies.on('changed', onChanged);
      check();
    });
  }

  async captureBitrixSession(preferredPortal?: string): Promise<BitrixSession | null> {
    const ses = session.fromPartition(SESSION_PARTITION);
    const all = await ses.cookies.get({});
    const relevant = all.filter((c) => c.domain != null && BITRIX_COOKIE_DOMAINS.some((d) => c.domain!.includes(d)));
    if (relevant.length === 0) return null;
    const bareHost = (domain: string): string => (domain.startsWith('.') ? domain.slice(1) : domain);
    const isPortalHost = (host: string): boolean =>
      /\.bitrix24\.(ru|com)$/.test(host) && host !== 'www.bitrix24.ru' && host !== 'www.bitrix24.com';
    let preferredHost = '';
    if (preferredPortal) {
      try {
        preferredHost = new URL(preferredPortal).hostname;
      } catch {
        preferredHost = '';
      }
    }
    const sessionHosts = relevant
      .filter((c) => c.name === BITRIX_SESSION_COOKIE)
      .map((c) => bareHost(c.domain!))
      .filter(isPortalHost);
    let portalHost = '';
    if (preferredHost && sessionHosts.includes(preferredHost)) portalHost = preferredHost;
    else if (sessionHosts.length > 0) portalHost = sessionHosts[0];
    if (!portalHost) return null;
    const portal = 'https://' + portalHost;
    const cookies: BitrixCookie[] = relevant.map((c) => {
      const host = bareHost(c.domain!);
      const hostUrl = host.includes('bitrix24.net') ? BITRIX_AUTH_NET_HOST : portal;
      return { name: c.name, value: c.value, host: hostUrl };
    });
    return { portal, cookies };
  }

  async clearAuthCookies(cookieDomains: string[], authCookieName: string): Promise<void> {
    const ses = session.fromPartition(SESSION_PARTITION);
    const matches = await ses.cookies.get({ name: authCookieName });
    for (const cookie of matches) {
      if (!cookie.domain || !cookieDomains.some((d) => cookie.domain!.includes(d))) continue;
      const host = cookie.domain.startsWith('.') ? cookie.domain.slice(1) : cookie.domain;
      const url = `https://${host}${cookie.path || '/'}`;
      try {
        await ses.cookies.remove(url, cookie.name);
      } catch (err) {
        console.log(`[COOKIES] failed to remove ${cookie.name} on ${url}:`, err);
      }
    }
  }

  async clearPlatformCookies(platform: Platform): Promise<number> {
    if (platform === Platform.Bitrix) {
      const removed = await this.removeCookiesForDomains(BITRIX_COOKIE_DOMAINS);
      const account = new BitrixCookieFile(this.cookieFilePath(Platform.Bitrix));
      const creds = await account.readCredentials();
      if (creds.email && creds.password) {
        await account.writeCredentials(creds.portal, creds.email, creds.password);
      } else {
        await fs.unlink(this.cookieFilePath(Platform.Bitrix)).catch(() => {});
      }
      console.log(`[COOKIES] cleared ${removed} bitrix cookies`);
      return removed;
    }
    const config = PLATFORM_CONFIG[platform];
    if (!config) return 0;
    const removed = await this.removeCookiesForDomains(config.cookieDomains);
    await fs.unlink(this.cookieFilePath(platform)).catch(() => {});
    console.log(`[COOKIES] cleared ${removed} cookies for ${platform}`);
    return removed;
  }

  private async removeCookiesForDomains(domains: string[]): Promise<number> {
    const ses = session.fromPartition(SESSION_PARTITION);
    const all = await ses.cookies.get({});
    let removed = 0;
    for (const cookie of all) {
      if (!cookie.domain || !domains.some((d) => cookie.domain!.includes(d))) continue;
      const host = cookie.domain.startsWith('.') ? cookie.domain.slice(1) : cookie.domain;
      const url = `https://${host}${cookie.path || '/'}`;
      try {
        await ses.cookies.remove(url, cookie.name);
        removed++;
      } catch (err) {
        console.log(`[COOKIES] failed to remove ${cookie.name} on ${url}:`, err);
      }
    }
    return removed;
  }

  async getDionCredentials(): Promise<DionCredentials> {
    return new DionCookieFile(this.cookieFilePath(Platform.Dion)).readCredentials();
  }

  async setDionCredentials(email: string, password: string): Promise<void> {
    await new DionCookieFile(this.cookieFilePath(Platform.Dion)).writeCredentials(email, password);
  }

  async getBitrixCredentials(): Promise<BitrixCredentials> {
    return new BitrixCookieFile(this.cookieFilePath(Platform.Bitrix)).readCredentials();
  }

  async setBitrixCredentials(portal: string, email: string, password: string): Promise<void> {
    await new BitrixCookieFile(this.cookieFilePath(Platform.Bitrix)).writeCredentials(portal, email, password);
  }

  async buildCookiesZip(): Promise<Buffer> {
    const platforms: { filename: string; domains: string[]; platform: Platform }[] = [
      { filename: 'cookies-vk.json', domains: VK_COOKIE_DOMAINS, platform: Platform.VK },
      { filename: 'cookies-yandex.json', domains: YANDEX_COOKIE_DOMAINS, platform: Platform.Telemost },
      { filename: 'cookies-dion.json', domains: DION_COOKIE_DOMAINS, platform: Platform.Dion },
      { filename: 'cookies-wbstream.json', domains: WBSTREAM_COOKIE_DOMAINS, platform: Platform.WBStream },
    ];
    const cookies = await Promise.all(platforms.map((p) => this.getCookiesForDomains(p.domains)));
    const dionContent = await new DionCookieFile(this.cookieFilePath(Platform.Dion)).readContent();
    const entries = platforms.map((p, i) => {
      const content = p.platform === Platform.Dion && dionContent ? dionContent : cookies[i];
      return { name: p.filename, data: Buffer.from(JSON.stringify(content, null, 2), 'utf8') };
    });
    const bitrixContent = await new BitrixCookieFile(this.cookieFilePath(Platform.Bitrix)).readRaw();
    entries.push({ name: 'cookies-bitrix.json', data: Buffer.from(JSON.stringify(bitrixContent ?? {}, null, 2), 'utf8') });
    return buildStoredZip(entries);
  }

  async setWBStreamDeviceId(id: string): Promise<void> {
    if (!id) return;
    const ses = session.fromPartition(SESSION_PARTITION);
    const existing = await ses.cookies.get({ url: 'https://stream.wb.ru/', name: '__wb_device_id' });
    if (existing.length > 0 && existing[0].value === id) return;
    try {
      await ses.cookies.set({
        url: 'https://stream.wb.ru/',
        name: '__wb_device_id',
        value: id,
        domain: 'stream.wb.ru',
        path: '/',
        secure: true,
        httpOnly: false,
        expirationDate: Math.floor(Date.now() / 1000) + 60 * 60 * 24 * 365 * 5,
      });
      console.log(`[wb-device-id] persisted ${id}`);
    } catch (err) {
      console.log(`[wb-device-id] failed to persist:`, err);
    }
  }
}
