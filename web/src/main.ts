// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

import './style.css';

const form = document.querySelector<HTMLFormElement>('#form')!;
const urlInput = document.querySelector<HTMLInputElement>('#url')!;
const keyInput = document.querySelector<HTMLInputElement>('#apiKey')!;
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
  profile_picture?: { url: string };
  background_image?: { url: string };
  websites?: { url: string; category?: string }[];
  verified?: boolean;
  influencer?: boolean;
  premium?: boolean;
}

interface Meta {
  retrieved_at: string;
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

function renderProfile(result: ProfileResult): void {
  const { data, meta } = result;
  output.replaceChildren();
  const card = el('article', 'card');

  if (data.background_image?.url) {
    const banner = el('div', 'banner');
    const img = el('img');
    img.src = data.background_image.url;
    img.alt = '';
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
    ['Influencer', data.influencer],
    ['Premium', data.premium],
  ];
  for (const [label, on] of badges) {
    if (on) nameRow.append(el('span', 'badge', label));
  }
  idBox.append(nameRow);
  if (data.headline) idBox.append(el('p', 'headline', data.headline));

  const bits: string[] = [];
  const place = data.location?.text || data.location?.country_code;
  if (place) bits.push(place);
  bits.push(meta.cached ? 'cached' : 'live');
  idBox.append(el('p', 'sub', bits.join(' · ')));
  idBox.append(link(data.profile_url, data.profile_url));
  head.append(idBox);
  card.append(head);

  if (data.summary) {
    const block = el('section', 'block');
    block.append(el('h3', undefined, 'About'));
    block.append(el('p', 'summary', data.summary));
    card.append(block);
  }

  if (data.websites?.length) {
    const block = el('section', 'block');
    block.append(el('h3', undefined, 'Websites'));
    const list = el('ul', 'links');
    for (const w of data.websites) {
      const item = el('li');
      item.append(link(w.url, w.url));
      if (w.category) item.append(el('span', 'muted', ` ${w.category.toLowerCase()}`));
      list.append(item);
    }
    block.append(list);
    card.append(block);
  }

  const details = el('details', 'raw');
  details.append(el('summary', undefined, 'Raw JSON'));
  details.append(el('pre', undefined, JSON.stringify(result, null, 2)));
  card.append(details);

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

async function fetchProfile(event: SubmitEvent): Promise<void> {
  event.preventDefault();

  const url = urlInput.value.trim();
  if (!url) {
    setStatus('Enter a LinkedIn profile URL.', 'error');
    return;
  }

  submit.disabled = true;
  output.replaceChildren();
  setStatus('Fetching…', 'pending');

  try {
    const headers: Record<string, string> = {};
    const key = keyInput.value.trim();
    if (key) {
      headers['X-API-Key'] = key;
    }

    const response = await fetch(`/v1/profile?url=${encodeURIComponent(url)}`, { headers });
    const raw = await response.text();

    if (response.ok) {
      renderProfile(JSON.parse(raw) as ProfileResult);
      setStatus(`OK (${response.status})`, 'ok');
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
    setStatus(`Request failed: ${(err as Error).message}`, 'error');
  } finally {
    submit.disabled = false;
  }
}

form.addEventListener('submit', fetchProfile);
