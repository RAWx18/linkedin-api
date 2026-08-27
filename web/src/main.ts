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
  first_name?: string;
  last_name?: string;
  full_name?: string;
  headline?: string;
  summary?: string;
  location?: { country_code?: string; text?: string };
  profile_language?: string;
  supported_locales?: string[];
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
  created_at?: string;
  experience?: Experience[];
  education?: Education[];
  skills?: string[];
  certifications?: Certification[];
  languages?: Language[];
  volunteer_experience?: VolunteerExperience[];
  projects?: Project[];
  test_scores?: TestScore[];
}

interface DateParts { year: number; month?: number; day?: number }
interface DateRange { start?: DateParts; end?: DateParts }
interface Experience { title?: string; company?: string; company_url?: string; location?: string; description?: string; date_range?: DateRange }
interface Education { school?: string; school_url?: string; degree?: string; field_of_study?: string; grade?: string; activities?: string; description?: string; date_range?: DateRange }
interface Certification { name?: string; authority?: string; authority_url?: string; url?: string; license_number?: string; date_range?: DateRange }
interface Language { name: string; proficiency?: string }
interface VolunteerExperience { role?: string; organization?: string; organization_url?: string; cause?: string; description?: string; date_range?: DateRange }
interface Project { title?: string; description?: string; date_range?: DateRange }
interface TestScore { name?: string; score?: string; description?: string; date?: DateParts }

interface Meta {
  retrieved_at: string;
  schema_version: string;
  source: string;
  cached: boolean;
  sections?: Record<string, string>;
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
  const block = el('section', 'panel');
  block.append(el('h3', undefined, title), ...children);
  return block;
}
const MONTHS = ['', 'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function fmtDate(d?: DateParts): string {
  if (!d?.year) return '';
  return d.month && MONTHS[d.month] ? `${MONTHS[d.month]} ${d.year}` : `${d.year}`;
}

function fmtRange(dr?: DateRange): string {
  if (!dr?.start) return '';
  const start = fmtDate(dr.start);
  const end = dr.end ? fmtDate(dr.end) : 'Present';
  return `${start} \u2013 ${end}`;
}

function joinMeta(...parts: (string | undefined)[]): string {
  return parts.filter((p): p is string => !!p).join(' \u00b7 ');
}

function humanize(s?: string): string {
  if (!s) return '';
  const lower = s.toLowerCase().replace(/_/g, ' ');
  return lower.charAt(0).toUpperCase() + lower.slice(1);
}

function entry(o: { title?: string; titleUrl?: string; sub?: string; subUrl?: string; meta?: string; body?: string }): HTMLElement {
  const item = el('div', 'entry');
  if (o.title) {
    const h = el('div', 'entry-title');
    h.append(o.titleUrl ? link(o.titleUrl, o.title) : document.createTextNode(o.title));
    item.append(h);
  }
  if (o.sub) {
    const s = el('div', 'entry-sub');
    s.append(o.subUrl ? link(o.subUrl, o.sub) : document.createTextNode(o.sub));
    item.append(s);
  }
  if (o.meta) item.append(el('div', 'entry-meta', o.meta));
  if (o.body) item.append(el('p', 'entry-body', o.body));
  return item;
}

function entries(items: HTMLElement[]): HTMLElement {
  const box = el('div', 'entries');
  box.append(...items);
  return box;
}
function imageItem(label: string, image: NonNullable<Profile['profile_picture']>): HTMLElement {
  const item = el('a', 'image-item');
  item.href = image.url;
  item.target = '_blank';
  item.rel = 'noopener noreferrer';
  const preview = el('img');
  preview.src = image.url;
  preview.alt = `${label} preview`;
  preview.loading = 'lazy';
  const copy = el('span');
  copy.append(el('strong', undefined, label));
  if (image.variants?.length) copy.append(el('small', undefined, `${image.variants.length} sizes`));
  item.append(preview, copy);
  return item;
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
  output.className = 'output result-grid';
  const card = el('article', 'profile-card');

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
  const name = data.full_name || [data.first_name, data.last_name].filter(Boolean).join(' ') || data.public_identifier;
  nameRow.append(el('h2', undefined, name));
  const badges: [string, boolean | undefined][] = [
    ['Verified', data.verified],
    ['Top Voice', data.top_voice],
    ['Influencer', data.influencer],
    ['Creator', data.creator],
    ['Premium', data.premium],
    ['Student', data.student],
    ['Memorialized', data.memorialized],
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
    const about = section('About', el('p', 'summary', data.summary));
    about.classList.add('about-panel');
    card.append(about);
  }

  if (data.experience?.length) {
    card.append(section('Experience', entries(data.experience.map((e) => entry({
      title: e.title,
      sub: e.company,
      subUrl: e.company_url,
      meta: joinMeta(fmtRange(e.date_range), e.location),
      body: e.description,
    })))));
  }

  if (data.education?.length) {
    card.append(section('Education', entries(data.education.map((e) => entry({
      title: e.school,
      titleUrl: e.school_url,
      sub: joinMeta(e.degree, e.field_of_study),
      meta: joinMeta(fmtRange(e.date_range), e.grade),
      body: joinMeta(e.description, e.activities),
    })))));
  }

  if (data.skills?.length) {
    const chips = el('div', 'chip-row');
    for (const s of data.skills) chips.append(el('span', 'badge', s));
    card.append(section('Skills', chips));
  }

  if (data.certifications?.length) {
    card.append(section('Certifications', entries(data.certifications.map((c) => entry({
      title: c.name,
      titleUrl: c.url,
      sub: c.authority,
      subUrl: c.authority_url,
      meta: joinMeta(fmtRange(c.date_range), c.license_number ? `ID ${c.license_number}` : undefined),
    })))));
  }

  if (data.languages?.length) {
    card.append(section('Languages', entries(data.languages.map((l) => entry({
      title: l.name,
      meta: humanize(l.proficiency),
    })))));
  }

  if (data.volunteer_experience?.length) {
    card.append(section('Volunteer experience', entries(data.volunteer_experience.map((v) => entry({
      title: v.role,
      sub: v.organization,
      subUrl: v.organization_url,
      meta: joinMeta(fmtRange(v.date_range), humanize(v.cause)),
      body: v.description,
    })))));
  }

  if (data.projects?.length) {
    card.append(section('Projects', entries(data.projects.map((p) => entry({
      title: p.title,
      meta: fmtRange(p.date_range),
      body: p.description,
    })))));
  }

  if (data.test_scores?.length) {
    card.append(section('Test scores', entries(data.test_scores.map((t) => entry({
      title: t.name,
      sub: t.score,
      meta: fmtDate(t.date),
      body: t.description,
    })))));
  }

  const primary = el('div', 'primary-stack');
  primary.append(card, jsonViewer(raw));
  output.append(primary);
  const side = el('aside', 'side-stack');

  if (data.websites?.length || data.creator_website) {
    const list = el('ul', 'link-list');
    for (const w of data.websites ?? []) {
      const item = el('li');
      item.append(link(w.url, w.url));
      if (w.category) item.append(el('span', 'muted', ` ${w.category.toLowerCase()}`));
      list.append(item);
    }
    if (data.creator_website) {
      const item = el('li');
      item.append(link(data.creator_website, data.creator_website), el('span', 'muted', ' creator'));
      list.append(item);
    }
    side.append(section('Links', list));
  }

  if (data.topics?.length) {
    const chips = el('div', 'chip-row');
    for (const t of data.topics) chips.append(el('span', 'badge', `#${t}`));
    side.append(section('Topics', chips));
  }

  if (data.profile_picture || data.background_image) {
    const images = el('div', 'image-list');
    if (data.profile_picture) images.append(imageItem('Profile image', data.profile_picture));
    if (data.background_image) images.append(imageItem('Background image', data.background_image));
    side.append(section('Images', images));
  }

  const metaRow = el('div', 'meta-row');
  const rows: [string, string][] = [
    ['retrieved', new Date(meta.retrieved_at).toLocaleString()],
    ['source', meta.source],
    ['schema', meta.schema_version],
    ['served', meta.cached ? 'cache' : 'live'],
  ];
  if (data.profile_language) rows.push(['language', data.profile_language]);
  if (data.supported_locales?.length) rows.push(['locales', data.supported_locales.join(', ')]);
  if (data.created_at) rows.push(['created', new Date(data.created_at).toLocaleDateString()]);
  for (const [term, value] of rows) {
    const chip = el('span', 'meta-chip');
    chip.append(el('span', 'k', term), el('span', 'v', value));
    metaRow.append(chip);
  }
  side.append(section('Metadata', metaRow));
  output.append(side);
}

function renderError(raw: string): void {
  output.replaceChildren();
  output.className = 'output';
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
  output.className = 'output';
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
