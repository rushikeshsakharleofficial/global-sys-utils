import { SITE } from '../config/site.js'

/* ── Theme ──────────────────────────────────────────────────── */
function initTheme() {
  const saved = localStorage.getItem('theme')
  const system = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  const theme = saved || system
  document.documentElement.setAttribute('data-theme', theme)
  updateThemeToggle(theme)
}

function toggleTheme() {
  const current = document.documentElement.getAttribute('data-theme') || 'dark'
  const next = current === 'dark' ? 'light' : 'dark'
  document.documentElement.setAttribute('data-theme', next)
  localStorage.setItem('theme', next)
  updateThemeToggle(next)
}

function updateThemeToggle(theme) {
  const btn = document.getElementById('theme-toggle')
  if (btn) btn.textContent = theme === 'dark' ? '☀' : '☾'
}

/* ── Nav injection ───────────────────────────────────────────── */
function getBase() {
  const meta = document.querySelector('meta[name="base-path"]')
  return meta ? meta.content : ''
}

function injectNav() {
  const base = getBase()
  const currentPage = location.pathname.split('/').pop() || 'index.html'
  const isIndex = currentPage === '' || currentPage === 'index.html'

  const links = SITE.nav.map(n => {
    const active = currentPage === n.href || (isIndex && false)
    return `<a href="${base}${n.href}" class="${active ? 'active' : ''}">${n.label}</a>`
  }).join('')

  const nav = document.createElement('nav')
  nav.className = 'site-nav'
  nav.id = 'site-nav'
  nav.innerHTML = `
    <div class="container">
      <a class="nav-brand" href="${base}index.html">
        <span class="nav-logo">${SITE.name}</span>
        <span class="nav-badge">${SITE.version}</span>
      </a>
      <div class="nav-links">${links}</div>
      <div class="nav-actions">
        <a class="nav-github" href="${SITE.repo}" target="_blank" rel="noopener">
          <svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16" aria-hidden="true">
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.012 8.012 0 0 0 16 8c0-4.42-3.58-8-8-8z"/>
          </svg>
          GitHub
        </a>
        <button class="theme-toggle" id="theme-toggle" aria-label="Toggle theme" onclick="window.__toggleTheme()">☀</button>
        <button class="nav-mobile-toggle" id="mobile-toggle" aria-label="Open menu" aria-expanded="false">☰</button>
      </div>
    </div>
  `

  const mobileMenu = document.createElement('div')
  mobileMenu.id = 'mobile-menu'
  mobileMenu.className = 'nav-mobile-menu'
  mobileMenu.innerHTML = SITE.nav.map(n =>
    `<a href="${base}${n.href}">${n.label}</a>`
  ).join('') + `<a href="${SITE.repo}" target="_blank" rel="noopener">GitHub ↗</a>`

  document.body.prepend(mobileMenu)
  document.body.prepend(nav)

  document.getElementById('mobile-toggle').addEventListener('click', function() {
    const menu = document.getElementById('mobile-menu')
    const open = menu.classList.toggle('open')
    this.setAttribute('aria-expanded', String(open))
    this.textContent = open ? '✕' : '☰'
  })

  markActiveNav()
}

function markActiveNav() {
  const current = location.pathname.split('/').pop() || 'index.html'
  document.querySelectorAll('.nav-links a, .nav-mobile-menu a').forEach(a => {
    const href = a.getAttribute('href') || ''
    const page = href.split('/').pop()
    if (page === current) a.classList.add('active')
  })
}

/* ── Footer injection ────────────────────────────────────────── */
function injectFooter() {
  const base = getBase()
  const footer = document.createElement('footer')
  footer.className = 'site-footer'
  footer.innerHTML = `
    <div class="container">
      <div class="footer-grid">
        <div class="footer-brand">
          <a class="nav-logo" href="${base}index.html">${SITE.name}</a>
          <p>${SITE.description}</p>
          <div class="footer-social">
            <a href="${SITE.social.github}" target="_blank" rel="noopener" aria-label="GitHub">
              <svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.012 8.012 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
            </a>
          </div>
        </div>
        <div class="footer-col">
          <h4>Product</h4>
          <div class="footer-links">
            <a href="${base}features.html">Features</a>
            <a href="${base}projects.html">Releases</a>
            <a href="${base}blog.html">Blog</a>
            <a href="${SITE.repo}" target="_blank" rel="noopener">GitHub</a>
          </div>
        </div>
        <div class="footer-col">
          <h4>Info</h4>
          <div class="footer-links">
            <a href="${base}about.html">About</a>
            <a href="${base}contact.html">Contact</a>
            <a href="${base}privacy.html">Privacy</a>
            <a href="${base}terms.html">Terms</a>
          </div>
        </div>
      </div>
      <div class="footer-bottom">
        <p>© ${new Date().getFullYear()} ${SITE.author}. Open source under MIT.</p>
        <p>Built with <a href="${SITE.repo}" target="_blank" rel="noopener">global-sys-utils</a></p>
      </div>
    </div>
  `
  document.body.appendChild(footer)
}

/* ── FAQ accordion ───────────────────────────────────────────── */
function initFAQ() {
  document.querySelectorAll('.faq-trigger').forEach(btn => {
    btn.addEventListener('click', () => {
      const expanded = btn.getAttribute('aria-expanded') === 'true'
      document.querySelectorAll('.faq-trigger').forEach(b => {
        b.setAttribute('aria-expanded', 'false')
        b.nextElementSibling.classList.remove('open')
      })
      if (!expanded) {
        btn.setAttribute('aria-expanded', 'true')
        btn.nextElementSibling.classList.add('open')
      }
    })
  })
}

/* ── Installation tabs ───────────────────────────────────────── */
function initTabs() {
  document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const target = btn.dataset.tab
      const container = btn.closest('[data-tabs]') || document
      container.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'))
      container.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'))
      btn.classList.add('active')
      const panel = container.querySelector(`[data-panel="${target}"]`)
      if (panel) panel.classList.add('active')
    })
  })
}

/* ── Render feature cards ────────────────────────────────────── */
function renderFeatures(containerId) {
  const el = document.getElementById(containerId)
  if (!el) return
  el.innerHTML = SITE.features.map(f => `
    <div class="card fade-in">
      <div class="card-icon">${f.icon}</div>
      <div class="card-title">${f.title}</div>
      <div class="card-body">${f.body}</div>
    </div>
  `).join('')
}

/* ── Render comparison table ─────────────────────────────────── */
function renderComparison(containerId) {
  const el = document.getElementById(containerId)
  if (!el) return
  const rows = SITE.comparison.map(row => {
    const lr = row.logrotate === true ? '<span class="check-yes">✓</span>'
      : row.logrotate === false ? '<span class="check-no">✗</span>'
      : `<span class="check-part">${row.logrotate}</span>`
    return `<tr><td>${row.capability}</td><td>${lr}</td><td><span class="check-yes">✓</span></td></tr>`
  }).join('')
  el.innerHTML = `
    <table class="comparison-table">
      <thead><tr><th>Capability</th><th>logrotate</th><th>global-sys-utils</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>
  `
}

/* ── Render testimonials ─────────────────────────────────────── */
function renderTestimonials(containerId) {
  const el = document.getElementById(containerId)
  if (!el) return
  el.innerHTML = `<div class="testimonials">${SITE.testimonials.map(t => `
    <div class="testimonial-card">
      <p class="testimonial-quote">${t.quote}</p>
      <div class="testimonial-meta">
        <div class="testimonial-name">${t.name}</div>
        <div class="testimonial-org">${t.org}</div>
      </div>
    </div>
  `).join('')}</div>`
}

/* ── Render FAQ ──────────────────────────────────────────────── */
function renderFAQ(containerId) {
  const el = document.getElementById(containerId)
  if (!el) return
  el.innerHTML = `<div class="faq-list">${SITE.faq.map((item, i) => `
    <div class="faq-item">
      <button class="faq-trigger" aria-expanded="false" aria-controls="faq-body-${i}">
        ${item.q}
        <span class="faq-icon" aria-hidden="true">+</span>
      </button>
      <div class="faq-body" id="faq-body-${i}" role="region">
        <div class="faq-body-inner">${item.a}</div>
      </div>
    </div>
  `).join('')}</div>`
  initFAQ()
}

/* ── Render blog cards ───────────────────────────────────────── */
function renderBlog(containerId, limit) {
  const el = document.getElementById(containerId)
  if (!el) return
  const base = getBase()
  const posts = limit ? SITE.blog.slice(0, limit) : SITE.blog
  el.innerHTML = posts.map(p => `
    <a class="blog-card" href="${base}${p.slug}.html" style="display:block;text-decoration:none;color:inherit">
      <div class="blog-date">${p.date}</div>
      ${p.badge ? `<span class="badge badge-primary" style="margin-bottom:8px">${p.badge}</span>` : ''}
      <h3 class="blog-title">${p.title}</h3>
      <p class="blog-body">${p.body}</p>
      <div class="blog-tags">${(p.tags || []).map(t => `<span class="badge badge-surface">${t}</span>`).join('')}</div>
      <div style="margin-top:12px;font-size:0.8rem;color:var(--primary);font-weight:500">Read more →</div>
    </a>
  `).join('')
}

/* ── Render projects / releases ──────────────────────────────── */
function renderProjects(containerId) {
  const el = document.getElementById(containerId)
  if (!el) return
  const base = getBase()
  el.innerHTML = SITE.projects.map(r => `
    <div class="release-card">
      <div class="release-header">
        <span class="release-version">${r.version}</span>
        ${r.badge ? `<span class="badge badge-secondary">${r.badge}</span>` : ''}
        <span class="release-date">${r.date}</span>
      </div>
      <h3 style="margin-bottom:8px">${r.title}</h3>
      <p class="release-body">${r.body}</p>
      <div class="blog-tags" style="margin-bottom:16px">${(r.tags || []).map(t => `<span class="badge badge-surface">${t}</span>`).join('')}</div>
      <div class="release-downloads">
        <a class="btn btn-primary btn-sm" href="${base}${r.deb}" download>⬇ DEB (${r.debArch || 'amd64'})</a>
        <a class="btn btn-ghost btn-sm" href="${base}${r.rpm}" download>⬇ RPM (${r.rpmArch || 'x86_64'})</a>
        <a class="btn btn-surface btn-sm" href="${SITE.repo}/releases" target="_blank" rel="noopener">GitHub Release</a>
      </div>
    </div>
  `).join('')
}

/* ── Expose toggle for inline onclick ────────────────────────── */
window.__toggleTheme = toggleTheme

/* ── Boot ────────────────────────────────────────────────────── */
document.addEventListener('DOMContentLoaded', () => {
  initTheme()
  injectNav()
  injectFooter()
  initTabs()

  renderFeatures('features-grid')
  renderComparison('comparison-grid')
  renderTestimonials('testimonials-grid')
  renderFAQ('faq-grid')
  renderBlog('blog-grid', 3)
  renderBlog('blog-all-grid')
  renderProjects('projects-grid')
})
