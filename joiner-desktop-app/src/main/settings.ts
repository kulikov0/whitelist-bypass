import { DUAL_TRACK_PLATFORMS, JOINER_PLATFORMS, JoinerSettings, RELIABLE_PLATFORMS } from '../constants';

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
  if (!JOINER_PLATFORMS.includes(value.platform)) throw new Error('Invalid platform');
  if (!['video', 'dc'].includes(value.tunnelMode)) throw new Error('Invalid tunnel mode');
  if (!['moderate', 'default', 'unlimited'].includes(value.resources)) throw new Error('Invalid resources');
  for (const [key, min, max] of [['socksPort', 1, 65535], ['vp8Fps', 1, 60], ['vp8Batch', 1, 256]] as const) {
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
  if (settings.dualTrack && DUAL_TRACK_PLATFORMS.includes(settings.platform)) args.push('--dual-track');
  if (settings.reliable && RELIABLE_PLATFORMS.includes(settings.platform)) args.push('--reliable');
  return args;
}

export function redactLog(text: string, settings: JoinerSettings | null): string {
  if (settings?.link) text = text.split(settings.link).join('[redacted]');
  return text;
}
