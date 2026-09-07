import * as fs from 'fs/promises';
import { BitrixCredentials } from '../types';

export interface BitrixCookie {
  name: string;
  value: string;
  host: string;
}

export class BitrixCookieFile {
  private filePath: string;

  constructor(filePath: string) {
    this.filePath = filePath;
  }

  async readCredentials(): Promise<BitrixCredentials> {
    const content = await this.read();
    return {
      portal: content?.portal || '',
      email: content?.email || '',
      password: content?.password || '',
    };
  }

  async writeCredentials(portal: string, email: string, password: string): Promise<void> {
    const content = {
      portal: portal || undefined,
      email: email || undefined,
      password: password || undefined,
      cookies: [] as unknown[],
    };
    await fs.writeFile(this.filePath, JSON.stringify(content));
  }

  async writeSession(portal: string, cookies: BitrixCookie[]): Promise<void> {
    const existing = await this.read();
    const content = {
      portal: portal || existing?.portal || undefined,
      email: existing?.email || undefined,
      password: existing?.password || undefined,
      cookies,
    };
    await fs.writeFile(this.filePath, JSON.stringify(content));
  }

  private async read(): Promise<BitrixCredentials | null> {
    try {
      const raw = await fs.readFile(this.filePath, 'utf8');
      const parsed = JSON.parse(raw);
      return {
        portal: typeof parsed.portal === 'string' ? parsed.portal : '',
        email: typeof parsed.email === 'string' ? parsed.email : '',
        password: typeof parsed.password === 'string' ? parsed.password : '',
      };
    } catch {
      return null;
    }
  }
}
