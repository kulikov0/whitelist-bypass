import { JoinerSettings } from '../constants';

export const DEFAULT_SETTINGS: JoinerSettings = {
  platform: 'telemost', link: '', displayName: 'Joiner', socksPort: 1080,
  socksUser: '', socksPass: '', tunnelMode: 'video', vp8Fps: 24,
  vp8Batch: 30, resources: 'default', dns: '1.1.1.1,8.8.8.8', noTun: false,
  dualTrack: false, reliable: false,
};

export function validateSettings(value: JoinerSettings): void {
  if (!value || typeof value !== 'object') throw new Error('Invalid settings');
  for (const key of ['link', 'displayName', 'socksUser', 'socksPass', 'dns'] as const) {
    if (typeof value[key] !== 'string') throw new Error(`Invalid ${key}`);
  }
  if (!value.link.trim()) throw new Error('Join link is required');
  if (!['telemost', 'wbstream', 'vk', 'dion', 'bitrix'].includes(value.platform)) throw new Error('Invalid platform');
  if (!['video', 'dc'].includes(value.tunnelMode)) throw new Error('Invalid tunnel mode');
  if (!['moderate', 'default', 'unlimited'].includes(value.resources)) throw new Error('Invalid resources');
  for (const [key, min, max] of [['socksPort', 1, 65535], ['vp8Fps', 1, 60], ['vp8Batch', 1, 3000]] as const) {
    if (!Number.isInteger(value[key]) || value[key] < min || value[key] > max) throw new Error(`Invalid ${key}`);
  }
  for (const key of ['noTun', 'dualTrack', 'reliable'] as const) {
    if (typeof value[key] !== 'boolean') throw new Error(`Invalid ${key}`);
  }
}

export function buildJoinerArgs(settings: JoinerSettings): string[] {
  validateSettings(settings);
  const args = [
    '--platform', settings.platform, '--link', settings.link,
    '--name', settings.displayName, '--socks-port', String(settings.socksPort),
    '--tunnel-mode', settings.tunnelMode, '--vp8-fps', String(settings.vp8Fps),
    '--vp8-batch', String(settings.vp8Batch), '--resources', settings.resources,
    '--dns', settings.dns,
  ];
  if (settings.socksUser) args.push('--socks-user', settings.socksUser);
  if (settings.socksPass) args.push('--socks-pass', settings.socksPass);
  if (settings.noTun) args.push('--no-tun');
  if (settings.dualTrack && ['vk', 'wbstream', 'bitrix'].includes(settings.platform)) args.push('--dual-track');
  if (settings.reliable && ['telemost', 'wbstream', 'bitrix'].includes(settings.platform)) args.push('--reliable');
  return args;
}

export function redactLog(text: string, settings: JoinerSettings | null): string {
  for (const secret of [settings?.link, settings?.socksPass]) {
    if (secret) text = text.split(secret).join('[redacted]');
  }
  return text;
}
