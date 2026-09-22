import { CallStatus, Platform } from '../../types';

export function parseCallStatus(msg: string): { tabId: string; status: CallStatus } | null {
  const prefix = '[CALL_STATUS] ';
  const idx = msg.indexOf(prefix);
  if (idx === -1) return null;
  const parts = msg.substring(idx + prefix.length);
  const colonIdx = parts.indexOf(':');
  if (colonIdx === -1) return null;
  const status = parts.substring(colonIdx + 1);
  return {
    tabId: parts.substring(0, colonIdx),
    status: status === CallStatus.Active ? CallStatus.Active : CallStatus.Inactive,
  };
}

export function extractTaggedCallLink(msg: string, platform: Platform): { tabId: string; link: string } | null {
  const tag = platform === Platform.Telemost ? 'Telemost' : 'VKCalls';
  const re = new RegExp('\\[BOT\\] ' + tag + '\\[([^\\]]*)\\]: call link:\\s*(.+)$');
  const match = msg.match(re);
  if (!match) return null;
  return { tabId: match[1].trim(), link: match[2].trim() };
}
