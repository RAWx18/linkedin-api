// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

import './style.css';

const form = document.querySelector<HTMLFormElement>('#form')!;
const urlInput = document.querySelector<HTMLInputElement>('#url')!;
const liAtInput = document.querySelector<HTMLInputElement>('#liAt')!;
const jsessionInput = document.querySelector<HTMLInputElement>('#jsession')!;
const uaInput = document.querySelector<HTMLInputElement>('#jsUserAgent')!;
const output = document.querySelector<HTMLElement>('#output')!;
const statusLine = document.querySelector<HTMLElement>('#status')!;
const submit = document.querySelector<HTMLButtonElement>('#submit')!;

type StatusKind = 'ok' | 'error' | 'pending';

interface Profile {
  public_identifier: string;
  profile_url: string;
  full_name?: string;
  headline?: string;
  summary?: string;
  location?: { country_code?: string; text?: string };
  profile_language?: string;
  profile_picture?: { url: string; variants?: { width: number; height?: number; url: string }[] };
  background_image?: { url: string; variants?: { width: number; height?: number; url: string }[] };
  websites?: { url: string; category?: string }[];
  creator_website?: string;
  topics?: string[];
  verified?: boolean;
  influencer?: boolean;
  premium?: boolean;
  creator?: boolean;
  top_voice?: boolean;
  student?: boolean;
  memorialized?: boolean;
}

interface Meta {
  retrieved_at: string;
  schema_version: string;
  source: string;
  cached: boolean;
}

interface ProfileResult {
  data: Profile;
  meta: Meta;
}

interface ErrorBody {
  error: { code: string; message: string; request_id?: string };
}

function setStatus(message: string, kind: StatusKind): void {
  statusLine.textContent = message;
  statusLine.className = kind;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function link(url: string, label: string): HTMLAnchorElement {
  const a = el('a', undefined, label);
  a.href = url;
  a.target = '_blank';
  a.rel = 'noopener noreferrer';
  return a;
}

function section(title: string, ...children: Node[]): HTMLElement {
  const block = el('section', 'block');
  block.append(el('h3', undefined, title), ...children);
  return block;
}

function jsonViewer(raw: string): HTMLElement {
  const block = el('section', 'block json-block');
  const bar = el('div', 'json-bar');
  bar.append(el('h3', undefined, 'JSON response'));
  const copy = el('button', 'copy-btn', 'Copy JSON');
  copy.type = 'button';
  copy.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(raw);
      copy.textContent = 'Copied';
      copy.classList.add('copied');
    } catch {
      copy.textContent = 'Copy failed';
    }
    setTimeout(() => {
      copy.textContent = 'Copy JSON';
      copy.classList.remove('copied');
    }, 1500);
  });
  bar.append(copy);
  let pretty = raw;
  try {
    pretty = JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    /* show the body as-is */
  }
  block.append(bar, el('pre', 'json-pre', pretty));
  return block;
}

function renderProfile(result: ProfileResult, raw: string): void {
  const { data, meta } = result;
  output.replaceChildren();
  const card = el('article', 'card');

  if (data.background_image?.url) {
    const banner = el('div', 'banner');
    const img = el('img');
    img.src = data.background_image.url;
    img.alt = '';
    img.loading = 'lazy';
    banner.append(img);
    card.append(banner);
  }

  const head = el('div', 'card-head');
  if (data.profile_picture?.url) {
    const avatar = el('img', 'avatar');
    avatar.src = data.profile_picture.url;
    avatar.alt = data.full_name ?? data.public_identifier;
    head.append(avatar);
  }

  const idBox = el('div', 'id-box');
  const nameRow = el('div', 'name-row');
  nameRow.append(el('h2', undefined, data.full_name || data.public_identifier));
  const badges: [string, boolean | undefined][] = [
    ['Verified', data.verified],
    ['Top Voice', data.top_voice],
    ['Influencer', data.influencer],
    ['Creator', data.creator],
    ['Premium', data.premium],
  ];
  for (const [label, on] of badges) {
    if (on) nameRow.append(el('span', 'badge', label));
  }
  idBox.append(nameRow);
  if (data.headline) idBox.append(el('p', 'headline', data.headline));
  const place = data.location?.text || data.location?.country_code;
  if (place) idBox.append(el('p', 'sub', place));
  idBox.append(link(data.profile_url, data.profile_url));
  head.append(idBox);
  card.append(head);

  if (data.summary) {
    card.append(section('About', el('p', 'summary', data.summary)));
  }

  if (data.websites?.length) {
    const list = el('ul', 'links');
    for (const w of data.websites) {
      const item = el('li');
      item.append(link(w.url, w.url));
      if (w.category) item.append(el('span', 'muted', ` ${w.category.toLowerCase()}`));
      list.append(item);
    }
    card.append(section('Websites', list));
  }

  if (data.creator_website) {
    card.append(section('Creator website', link(data.creator_website, data.creator_website)));
  }

  if (data.topics?.length) {
    const chips = el('div', 'name-row');
    for (const t of data.topics) chips.append(el('span', 'badge', `#${t}`));
    card.append(section('Topics', chips));
  }

  const metaRow = el('div', 'meta-row');
  const rows: [string, string][] = [
    ['retrieved', new Date(meta.retrieved_at).toLocaleString()],
    ['source', meta.source],
    ['schema', meta.schema_version],
    ['served', meta.cached ? 'cache' : 'live'],
  ];
  for (const [term, value] of rows) {
    const chip = el('span', 'meta-chip');
    chip.append(el('span', 'k', term), el('span', 'v', value));
    metaRow.append(chip);
  }
  card.append(section('Metadata', metaRow));

  card.append(jsonViewer(raw));
  output.append(card);
}

function renderError(raw: string): void {
  output.replaceChildren();
  let message = raw;
  try {
    const env = JSON.parse(raw) as ErrorBody;
    message = `${env.error.code}: ${env.error.message}`;
    if (env.error.request_id) {
      message += ` (request ${env.error.request_id})`;
    }
  } catch {
    /* leave the raw body as-is */
  }
  output.append(el('div', 'error-box', message));
}

function renderLoading(): void {
  output.replaceChildren();
  const lines = el('div', 'skeleton-lines');
  lines.append(el('div', 'skeleton-line wide'), el('div', 'skeleton-line'), el('div', 'skeleton-line narrow'));
  const skeleton = el('div', 'skeleton');
  skeleton.append(el('div', 'skeleton-avatar'), lines);
  output.append(skeleton);
}

async function fetchProfile(event: SubmitEvent): Promise<void> {
  event.preventDefault();

  const url = urlInput.value.trim();
  if (!url) {
    setStatus('Enter a LinkedIn profile URL.', 'error');
    return;
  }

  const liAt = liAtInput.value.trim();
  const jsession = jsessionInput.value.trim();
  if ((liAt === '') !== (jsession === '')) {
    setStatus('Provide both li_at and JSESSIONID to use your own session, or leave both blank.', 'error');
    return;
  }

  submit.disabled = true;
  setStatus('Fetching profile…', 'pending');
  renderLoading();

  try {
    const headers: Record<string, string> = {};
    if (liAt && jsession) {
      headers['X-LinkedIn-Li-At'] = liAt;
      headers['X-LinkedIn-JSESSIONID'] = jsession;
      const ua = uaInput.value.trim();
      if (ua) {
        headers['X-LinkedIn-User-Agent'] = ua;
      }
    }

    const response = await fetch(`/v1/profile?url=${encodeURIComponent(url)}`, { headers });
    const raw = await response.text();

    if (response.ok) {
      renderProfile(JSON.parse(raw) as ProfileResult, raw);
      setStatus('Profile retrieved.', 'ok');
      return;
    }

    if (response.status === 429) {
      const retry = response.headers.get('Retry-After');
      setStatus(retry ? `Rate limited. Retry in ${retry}s.` : 'Rate limited. Slow down.', 'error');
    } else {
      setStatus(`Error (${response.status})`, 'error');
    }
    renderError(raw);
  } catch (err) {
    output.replaceChildren();
    setStatus(`Request failed: ${(err as Error).message}`, 'error');
  } finally {
    submit.disabled = false;
    // Never retain caller session credentials client-side.
    liAtInput.value = '';
    jsessionInput.value = '';
    uaInput.value = '';
  }
}

form.addEventListener('submit', fetchProfile);
