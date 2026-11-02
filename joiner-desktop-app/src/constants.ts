export const IPC = {
  STATE: 'joiner:state',
  START: 'joiner:start',
  STOP: 'joiner:stop',
  LOG: 'joiner:log',
  STATUS: 'joiner:status',
  RUNNING: 'joiner:running',
  LOGIN_LINK: 'joiner:login-link',
} as const;

export const JOINER_PLATFORMS = ['wbstream', 'telemost', 'vk', 'dion', 'bitrix', 'blink'] as const;
export type JoinerPlatform = typeof JOINER_PLATFORMS[number];

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
