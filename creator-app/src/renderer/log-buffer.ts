import { MAX_LOG_CHARS, LOG_TRIM_KEEP_CHARS } from '../constants';

export function trimLogText(text: string): string {
  if (text.length <= MAX_LOG_CHARS) return text;
  const cut = text.length - LOG_TRIM_KEEP_CHARS;
  const newline = text.indexOf('\n', cut);
  return text.slice(newline === -1 ? cut : newline + 1);
}

export function appendLogText(text: string, msg: string): string {
  return trimLogText(text ? text + '\n' + msg : msg);
}

export function appendLogElement(el: HTMLElement, msg: string): void {
  el.textContent = appendLogText(el.textContent || '', msg);
  el.scrollTop = el.scrollHeight;
}
