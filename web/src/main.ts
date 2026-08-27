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

function safeURL(url: string): string | undefined {
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : undefined;
  } catch {
    return undefined;
  }
}

function previewImageURL(url: string): string | undefined {
  const href = safeURL(url);
  if (!href) return undefined;
  const parsed = new URL(href);
  return parsed.protocol === 'https:' && parsed.host === 'media.licdn.com'
    ? `/v1/image?url=${encodeURIComponent(href)}`
    : href;
}

function link(url: string, label: string): HTMLElement {
  const href = safeURL(url);
  if (!href) return el('span', 'invalid-link', label);
  const a = el('a', undefined, label);
  a.href = href;
  a.target = '_blank';
  a.rel = 'noopener noreferrer';
  return a;
}

function section(title: string, ...children: Node[]): HTMLElement {
  const block = el('section', 'panel');
  block.append(el('h3', undefined, title), ...children);
  return block;
}

function collection(title: string, count: number, content: Node, open = false): HTMLDetailsElement {
  const panel = el('details', 'panel collection-panel');
  panel.open = open;
  const summary = el('summary');
  summary.append(el('span', 'section-title', title), el('span', 'section-count', `${count}`));
  panel.append(summary, content);
  return panel;
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
  if (o.body) {
    const description = el('details', 'entry-description');
    description.append(el('summary', undefined, 'Description'), el('p', 'entry-body', o.body));
    item.append(description);
  }
  return item;
}

function entries(items: HTMLElement[]): HTMLElement {
  const box = el('div', 'entries');
  box.append(...items);
  return box;
}

function responsiveImage(
  image: NonNullable<Profile['profile_picture']>,
  alt: string,
  className: string,
  displayWidth: number,
  sizes: string,
): HTMLImageElement | undefined {
  const variants = (image.variants ?? [])
    .map((variant) => ({ width: variant.width, url: previewImageURL(variant.url) }))
    .filter((variant): variant is { width: number; url: string } => variant.width > 0 && !!variant.url)
    .sort((left, right) => left.width - right.width)
    .filter((variant, index, all) => index === 0 || variant.width !== all[index - 1].width);
  const baseURL = previewImageURL(image.url);
  if (!baseURL && variants.length === 0) return undefined;

  const preferred = variants.find((variant) => variant.width >= displayWidth * 2) ?? variants.at(-1);
  const img = el('img', className);
  img.src = preferred?.url ?? baseURL!;
  img.alt = alt;
  img.decoding = 'async';
  img.loading = 'eager';
  if (variants.length) {
    img.srcset = variants.map((variant) => `${variant.url} ${variant.width}w`).join(', ');
    img.sizes = sizes;
  }

  const fallbacks = [...new Set([baseURL, ...[...variants].reverse().map((variant) => variant.url)].filter(Boolean))] as string[];
  const attempted = new Set<string>();
  img.addEventListener('error', () => {
    attempted.add(img.currentSrc || img.src);
    img.removeAttribute('srcset');
    const fallback = fallbacks.find((url) => !attempted.has(url));
    if (fallback) {
      img.src = fallback;
      return;
    }
    img.hidden = true;
    img.parentElement?.classList.add('image-failed');
  });
  return img;
}

function jsonViewer(raw: string): HTMLElement {
  const block = el('section', 'workspace-panel json-panel');
  const bar = el('div', 'json-bar');
  bar.append(el('h2', undefined, 'JSON response'));
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

async function renderProfile(result: ProfileResult, raw: string): Promise<void> {
  const { data, meta } = result;
  output.replaceChildren();
  output.className = 'output result-workspace';
  const preview = el('section', 'workspace-panel preview-panel');
  const previewBar = el('div', 'workspace-bar');
  previewBar.append(el('h2', undefined, 'Profile preview'));
  const previewBody = el('div', 'preview-body');
  const card = el('article', 'profile-card');
  const name = data.full_name || [data.first_name, data.last_name].filter(Boolean).join(' ') || data.public_identifier;

  const banner = el('div', 'banner');
  if (data.background_image) {
    const background = responsiveImage(
      data.background_image,
      '',
      'banner-image',
      560,
      '(max-width: 860px) calc(100vw - 2rem), 560px',
    );
    if (background) {
      banner.append(background);
    }
  }
  card.append(banner);

  const head = el('div', 'card-head');
  const avatarFrame = el('div', 'avatar-frame');
  avatarFrame.append(el('span', 'avatar-fallback', name.charAt(0).toUpperCase()));
  if (data.profile_picture) {
    const avatar = responsiveImage(data.profile_picture, name, 'avatar', 84, '84px');
    if (avatar) {
      avatarFrame.append(avatar);
    }
  }
  head.append(avatarFrame);

  const idBox = el('div', 'id-box');
  const nameRow = el('div', 'name-row');
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

  previewBody.append(card);
  const previewSections = el('div', 'preview-sections');
  const main = el('div', 'main-stack');
  const side = el('aside', 'side-stack');

  if (data.experience?.length) {
    main.append(collection('Experience', data.experience.length, entries(data.experience.map((e) => entry({
      title: e.title,
      sub: e.company,
      subUrl: e.company_url,
      meta: joinMeta(fmtRange(e.date_range), e.location),
      body: e.description,
    }))), true));
  }

  if (data.education?.length) {
    side.append(collection('Education', data.education.length, entries(data.education.map((e) => entry({
      title: e.school,
      titleUrl: e.school_url,
      sub: joinMeta(e.degree, e.field_of_study),
      meta: joinMeta(fmtRange(e.date_range), e.grade),
      body: joinMeta(e.description, e.activities),
    })))));
  }

  if (data.skills?.length) {
    const chips = el('div', 'chip-row');
    for (const skill of data.skills) chips.append(el('span', 'tag', skill));
    side.append(collection('Skills', data.skills.length, chips));
  }

  if (data.certifications?.length) {
    side.append(collection('Certifications', data.certifications.length, entries(data.certifications.map((c) => entry({
      title: c.name,
      titleUrl: c.url,
      sub: c.authority,
      subUrl: c.authority_url,
      meta: joinMeta(fmtRange(c.date_range), c.license_number ? `ID ${c.license_number}` : undefined),
    })))));
  }

  if (data.languages?.length) {
    side.append(collection('Languages', data.languages.length, entries(data.languages.map((l) => entry({
      title: l.name,
      meta: humanize(l.proficiency),
    })))));
  }

  if (data.volunteer_experience?.length) {
    main.append(collection('Volunteer experience', data.volunteer_experience.length, entries(data.volunteer_experience.map((v) => entry({
      title: v.role,
      sub: v.organization,
      subUrl: v.organization_url,
      meta: joinMeta(fmtRange(v.date_range), humanize(v.cause)),
      body: v.description,
    })))));
  }

  if (data.projects?.length) {
    main.append(collection('Projects', data.projects.length, entries(data.projects.map((p) => entry({
      title: p.title,
      meta: fmtRange(p.date_range),
      body: p.description,
    })))));
  }

  if (data.test_scores?.length) {
    side.append(collection('Test scores', data.test_scores.length, entries(data.test_scores.map((t) => entry({
      title: t.name,
      sub: t.score,
      meta: fmtDate(t.date),
      body: t.description,
    })))));
  }

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
    side.append(collection('Links', list.childElementCount, list));
  }

  if (data.topics?.length) {
    const chips = el('div', 'chip-row');
    for (const topic of data.topics) chips.append(el('span', 'tag', `#${topic}`));
    side.append(collection('Topics', data.topics.length, chips));
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
    const row = el('div', 'meta-item');
    row.append(el('span', 'k', term), el('span', 'v', value));
    metaRow.append(row);
  }
  side.append(collection('Metadata', rows.length, metaRow));

  const unavailable = Object.entries(meta.sections ?? {})
    .filter(([, status]) => status === 'unavailable')
    .map(([name]) => humanize(name));
  if (unavailable.length) {
    side.prepend(section('Partial data', el('p', 'notice', `${unavailable.join(', ')} unavailable for this lookup.`)));
  }

  if (main.childElementCount) previewSections.append(main);
  previewSections.append(side);
  previewBody.append(previewSections);
  preview.append(previewBar, previewBody);
  output.append(preview);
  output.append(jsonViewer(raw));

  await Promise.all([...card.querySelectorAll('img')].map((image) => new Promise<void>((resolve) => {
    if (image.complete) {
      resolve();
      return;
    }
    image.addEventListener('load', () => resolve(), { once: true });
    image.addEventListener('error', () => {
      if (image.hidden) resolve();
    });
    setTimeout(resolve, 5000);
  })));
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
      await renderProfile(JSON.parse(raw) as ProfileResult, raw);
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
