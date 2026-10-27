// Shared, anonymous product measurement. Configuration is public; previews fail closed.
(() => {
  "use strict";

  if (window.productAnalyticsInitialized) return;

  const script = document.currentScript;
  const products = {
    vacancyd: ["vacancyd.com", "docs.vacancyd.com", "/"],
    platefold: ["platefold.page", "maxfield-allison.github.io", "/platefold/"],
    dnsweaver: ["dnsweaver.com", "maxfield-allison.github.io", "/dnsweaver/"],
  };
  const config = { ...script?.dataset };
  const tuple = products[config?.product];
  if (!tuple || !/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(config.websiteId || "")) return;
  if (config.appHost !== tuple[0] || config.docsHost !== tuple[1] || config.docsPrefix !== tuple[2]) return;
  const surface = config.surface;
  const host = surface === "site" ? tuple[0] : surface === "docs" ? tuple[1] : "";
  if (location.protocol !== "https:" || location.host !== host) return;
  if (surface === "docs" && !location.pathname.startsWith(tuple[2])) return;

  const campaignKeys = ["utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term"];
  let campaigns;
  try {
    campaigns = JSON.parse(config.utmValues || "{}");
    if (!campaignKeys.every((key) => campaigns[key] === undefined ||
      (Array.isArray(campaigns[key]) && campaigns[key].length <= 40 &&
        campaigns[key].every((value) => typeof value === "string" && /^[a-z0-9][a-z0-9_-]{0,47}$/.test(value))))) return;
  } catch {
    return;
  }

  function optedOut() {
    const dnt = [window.doNotTrack, navigator.doNotTrack, navigator.msDoNotTrack];
    if (dnt.some((value) => [1, "1", "yes"].includes(value)) || navigator.globalPrivacyControl === true) return true;
    try {
      return Boolean(localStorage.getItem("umami.disabled"));
    } catch {
      // Blocked storage also breaks this tracker version's per-send opt-out read.
      // Skip measurement, keeping the rest of the page usable.
      return true;
    }
  }
  if (optedOut()) return;
  // One boolean per document prevents duplicate collectors/listeners; it is no identity.
  window.productAnalyticsInitialized = true;

  function parseUrl(value, base = location.href, allowHttp = false) {
    if (typeof value !== "string" || !value) return null;
    try {
      const url = new URL(value, base);
      const protocolAllowed = url.protocol === "https:" || (allowHttp && url.protocol === "http:");
      if (!protocolAllowed || url.username || url.password || url.port) return null;
      return url;
    } catch {
      return null;
    }
  }

  function normalizedPath(url) {
    if (url.protocol !== "https:") return null;
    if (url.hostname === tuple[0]) return url.pathname;
    if (url.hostname !== tuple[1] || !url.pathname.startsWith(tuple[2])) return null;
    return `/docs/${url.pathname.slice(tuple[2].length)}`;
  }

  function readPage() {
    const links = document.querySelectorAll('link[rel="canonical"]');
    const href = links.length === 1 ? links[0].getAttribute("href") : "";
    const canonical = href?.startsWith("https://") ? parseUrl(href) : null;
    // The rendered canonical, rather than an arbitrary request path, defines the page.
    const valid = canonical && canonical.hostname === host && !canonical.search && !canonical.hash &&
      /^\/[a-zA-Z0-9/_.,-]{0,199}$/.test(canonical.pathname) && normalizedPath(canonical) !== null;
    const path = valid ? normalizedPath(canonical) : "/404.html";
    const query = new URLSearchParams();
    if (valid && location.pathname === canonical.pathname) {
      const incoming = new URLSearchParams(location.search);
      for (const key of campaignKeys) {
        const values = incoming.getAll(key);
        if (values.length === 1 && campaigns[key]?.includes(values[0])) query.set(key, values[0]);
      }
    }
    return { path, url: path + (query.size ? `?${query}` : ""), canonical: valid ? canonical : null };
  }

  const permitted = new WeakSet();
  const eventNames = new Set(["link-click", "docs-search", "copy-code", "scroll-depth"]);
  const queue = [];
  let ready = false;
  let page;
  let previousPath;
  let seen = new Set();
  let referrer = "";
  const originalReferrer = parseUrl(document.referrer, location.href, true);
  if (originalReferrer) referrer = originalReferrer.origin;

  // Only objects created inside this closure may reach the collector. Public track(),
  // identify(), auto events and arbitrary override callbacks cannot add data.
  window.productAnalyticsBeforeSend = (type, payload) => {
    if (type !== "event" || !permitted.has(payload) || optedOut()) return false;
    permitted.delete(payload);
    if (payload.website !== config.websiteId || payload.hostname !== host) return false;
    if (payload.name && !eventNames.has(payload.name)) return false;
    return payload;
  };

  function deliver(payload) {
    if (optedOut() || !window.umami?.track) return;
    permitted.add(payload);
    try {
      // The callback preserves object identity across Umami's track boundary.
      Promise.resolve(window.umami.track(() => payload)).catch(() => {});
    } catch {
      // Analytics failure must never interrupt the application or its navigation.
    }
  }

  function track(name, data = {}) {
    if (!page || optedOut()) return;
    const payload = {
      website: config.websiteId,
      hostname: host,
      url: page.url,
      referrer,
      screen: `${screen.width}x${screen.height}`,
      language: navigator.language,
      data: { product: config.product, surface, ...data },
    };
    if (name) payload.name = name;
    if (ready) deliver(payload);
    else if (queue.length < 32) queue.push(payload);
  }

  function navigate() {
    const next = readPage();
    // Ignore duplicate initial document$ emissions; A -> B -> A remains three views.
    if (page?.path === next.path) return;
    if (page) {
      previousPath = page.path;
      referrer = previousPath === "/404.html" ? location.origin : `${location.origin}${previousPath}`;
    }
    page = next;
    seen = new Set();
    track();
  }

  function placement(element) {
    if (element.closest("footer, [role=contentinfo]")) return "footer";
    if (element.closest("nav, [role=navigation], header")) return "nav";
    if (element.closest(".hero, [data-analytics-primary]")) return "primary";
    return "body";
  }

  function classifyLink(link) {
    const href = link.getAttribute("href");
    if (!href || href.startsWith("#")) return null;
    const url = parseUrl(href, page.canonical?.href || `${location.origin}/404.html`, true);
    if (!url) return null;
    const path = normalizedPath(url);
    const destination = { destination_origin: url.origin };
    let action = "external";
    if (path !== null) {
      // Only a rendered link supplies this destination path; never use location's raw path.
      if (/^\/[a-zA-Z0-9/_.,-]{0,199}$/.test(path)) destination.destination_path = path;
      action = url.hostname === tuple[1] ? "docs" : "internal";
      if (link.hasAttribute("download")) action = "download";
      if (/\/(?:sample|examples?)(?:\/|\.)/.test(path)) action = "sample";
    } else if (url.protocol === "https:" && url.hostname === "github.com" && (url.pathname === `/maxfield-allison/${config.product}` ||
      url.pathname.startsWith(`/maxfield-allison/${config.product}/`))) {
      const root = `/maxfield-allison/${config.product}`;
      const releasePath = `${root}/releases`;
      action = "source";
      if (/^\/download\/[^/]+\/[^/]+$/.test(url.pathname.slice(releasePath.length))) action = "download";
      else if (url.pathname === releasePath || url.pathname === `${releasePath}/` ||
        url.pathname === `${releasePath}/latest` || url.pathname.startsWith(`${releasePath}/tag/`)) action = "releases";
      destination.destination_path = ["download", "releases"].includes(action) ? releasePath : root;
    } else if (url.protocol === "https:" && url.hostname === "github.com" && url.pathname === "/sponsors/maxfield-allison") {
      action = "sponsor";
      destination.destination_path = "/sponsors/maxfield-allison";
    } else if (url.protocol === "https:" && url.hostname === "probablyfine.dev" && /^\/support\/?$/.test(url.pathname)) {
      action = "support";
      destination.destination_path = "/support/";
    }
    return { action, placement: placement(link), ...destination };
  }

  function click(event) {
    const element = event.target instanceof Element ? event.target : null;
    if (!element) return;
    const copy = element.closest(".md-clipboard, [data-clipboard-target], [data-copy-code]");
    if (surface === "docs" && copy) track("copy-code");
    const link = element.closest("a[href]");
    if (!link) return;
    const data = classifyLink(link);
    if (data) track("link-click", data);
  }

  function isDocsSearchInput(element) {
    if (!(element instanceof Element)) return false;
    if (element.matches('[data-md-component="search-query"], input[type="search"]')) return true;
    // Zensical renders its accessible search combobox in an open shadow dialog.
    // Require that known search UI, rather than treating every combobox as search.
    const root = element.getRootNode();
    if (!element.matches('input[role="combobox"]') || !(root instanceof ShadowRoot) ||
      root.host.tagName !== "DIV" || !document.querySelector('[data-md-component="search"] > button.md-search__button')) return false;
    if (element.closest('dialog[aria-label="Search"][aria-modal="true"]')) return true;
    // The pinned 0.0.69 UI predates the accessible dialog. Match its static English
    // search metadata; remove this adapter when that renderer is upgraded.
    return Boolean(document.querySelector('meta[name="generator"][content="zensical-0.0.69"]')) &&
      element.matches('input[type="text"][role="combobox"][placeholder="Search"][autocomplete="off"]');
  }

  function search(event) {
    if (surface !== "docs" || seen.has("search")) return;
    if (!event.composedPath().some(isDocsSearchInput)) return;
    seen.add("search");
    track("docs-search");
  }

  function scroll(event) {
    const target = event.target instanceof Element ? event.target : document.scrollingElement;
    if (!target || (target !== document.scrollingElement && !target.matches("main, [role=main], .md-content"))) return;
    const range = target.scrollHeight - target.clientHeight;
    if (range <= 0) return;
    const progress = target.scrollTop / range;
    for (const depth of [50, 90]) {
      if (progress < depth / 100 || seen.has(depth)) continue;
      seen.add(depth);
      track("scroll-depth", { depth });
    }
  }

  function start() {
    if (optedOut()) return;
    navigate();
    document.addEventListener("click", click, { passive: true });
    document.addEventListener("input", search, { passive: true });
    document.addEventListener("scroll", scroll, { passive: true, capture: true });
    if (window.document$?.subscribe) window.document$.subscribe(navigate);
    const tracker = document.createElement("script");
    tracker.src = "https://probablyfine.dev/pf-insights/script.js";
    tracker.defer = true;
    Object.assign(tracker.dataset, {
      websiteId: config.websiteId,
      domains: host,
      autoTrack: "false",
      excludeSearch: "true",
      excludeHash: "true",
      doNotTrack: "true",
      beforeSend: "productAnalyticsBeforeSend",
    });
    tracker.onload = () => {
      ready = true;
      for (const payload of queue.splice(0)) deliver(payload);
    };
    tracker.onerror = () => { queue.length = 0; };
    document.head.append(tracker);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start, { once: true });
  else start();
})();
