import { TabListEntry, BotCommand, CallStatus } from '../types';

export function generateShortId(tabId: string): number {
  let hash = 0;
  for (let i = 0; i < tabId.length; i++) {
    hash = ((hash << 5) - hash) + tabId.charCodeAt(i);
    hash = hash & hash;
  }
  return Math.abs(hash) % 10000;
}

export function padShortId(id: number): string {
  return ('000' + id).slice(-4);
}

export function findTabByShortId(tabsList: TabListEntry[], shortId: string): TabListEntry | null {
  const target = parseInt(shortId, 10);
  for (const tab of tabsList) {
    if (generateShortId(tab.id) === target) return tab;
  }
  return null;
}

export function createMainKeyboard() {
  return {
    one_time: false,
    buttons: [
      [
        { action: { type: 'text', label: 'VK DC', payload: JSON.stringify({ cmd: BotCommand.VK, mode: 'dc' }) } },
        { action: { type: 'text', label: 'VK Video', payload: JSON.stringify({ cmd: BotCommand.VK, mode: 'video' }) } },
      ],
      [
        { action: { type: 'text', label: 'TM DC', payload: JSON.stringify({ cmd: BotCommand.TM, mode: 'dc' }) } },
        { action: { type: 'text', label: 'TM Video', payload: JSON.stringify({ cmd: BotCommand.TM, mode: 'video' }) } },
      ],
      [
        { action: { type: 'text', label: 'Active Tabs', payload: JSON.stringify({ cmd: BotCommand.List }) } },
      ],
    ],
  };
}

export function createListKeyboard(tabsList: TabListEntry[]) {
  const buttons = tabsList.map((entry) => {
    const shortId = padShortId(generateShortId(entry.id));
    const prefix = entry.isBot ? 'bot' : 'user';
    const status = entry.callStatus === CallStatus.Active ? '+' : '-';
    return [
      {
        action: {
          type: 'text',
          label: `${prefix} ${entry.platform} ${entry.mode} ${shortId} ${status}`,
          payload: JSON.stringify({ cmd: BotCommand.Close, id: entry.id }),
        },
      },
    ];
  });
  buttons.push([
    { action: { type: 'text', label: 'Back', payload: JSON.stringify({ cmd: BotCommand.Menu }) } },
  ]);
  return { one_time: false, buttons };
}
