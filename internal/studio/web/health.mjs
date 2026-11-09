import { html, List } from './components.mjs';
import { healthQueue } from './logic.mjs';

function Section({ section, open }) {
  return html`<details class="card" open=${open}>
    <summary><span class=${`dot ${section.tone ?? ''}`}></span><b>${section.title}</b> <span class="count">${section.items.length}</span></summary>
    ${section.hint ? html`<p class="hint">${section.hint}</p>` : null}
    <${List} items=${section.items} render=${(item) => html`<a class="row" key=${`${item.sub}${item.detail}`} href=${item.href ?? '#/health'}>
      <span class="row-main">${item.label}<small class="mono">${item.sub}</small></span><span class="muted row-detail">${item.detail}</span></a>`} />
  </details>`;
}

export function Health({ state, model }) {
  const { sections, backlog } = healthQueue(state, model);
  return html`<section class="page">
    <h1>Health</h1>
    <p class="muted">Problems to fix first, then optional editorial work.</p>
    <div class="stats">
      ${[['Skills', model.skills.size], ['Technologies', model.technologies.size], ['Mappings', model.mappings.size], ['Repositories', model.repositories.size]].map(([label, value]) => html`<div class="stat"><b>${value}</b><span>${label}</span></div>`)}
    </div>
    ${sections.length ? sections.map((section) => html`<${Section} key=${section.id} section=${section} open=${['errors', 'upstream'].includes(section.id)} />`) : html`<p class="card pad">No problems found.</p>`}
    <h2 class="section-title">Optional backlog</h2>
    <p class="muted">Valid registry states that may reward curation. None of these block publishing.</p>
    ${backlog.map((section) => html`<${Section} key=${section.id} section=${section} open=${false} />`)}
  </section>`;
}
