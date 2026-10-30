export const IPC = {
  START: 'joiner:start',
  STOP: 'joiner:stop',
  LOG: 'joiner:log',
  STATUS: 'joiner:status',
  RUNNING: 'joiner:running',
  LOGIN_LINK: 'joiner:login-link',
} as const;

export type JoinerPlatform = 'wbstream' | 'telemost' | 'vk' | 'dion' | 'bitrix' | 'blink';

export interface LoginLink {
  link: string;
  qr: string;
}

export interface JoinerSettings {
  platform: JoinerPlatform;
  link: string;
  displayName: string;
  socksPort: number;
  socksUser: string;
  socksPass: string;
  tunnelMode: 'video' | 'dc';
  vp8Fps: number;
  vp8Batch: number;
  resources: 'moderate' | 'default' | 'unlimited';
  dns: string;
  noTun: boolean;
  dualTrack: boolean;
  reliable: boolean;
}
