// Mermaid theme initialization for MkDocs Material dark/light mode
// https://squidfunk.github.io/mkdocs-material/reference/diagrams/

document.addEventListener('DOMContentLoaded', function() {
  // Get the current color scheme
  const getPreferredTheme = () => {
    const palette = document.querySelector('[data-md-color-scheme]');
    return palette?.dataset.mdColorScheme === 'slate' ? 'dark' : 'default';
  };

  // Initialize Mermaid with theme
  if (typeof mermaid !== 'undefined') {
    mermaid.initialize({
      startOnLoad: true,
      theme: getPreferredTheme(),
      securityLevel: 'loose',
    });
  }
});

// Re-render diagrams on theme change
const observer = new MutationObserver((mutations) => {
  mutations.forEach((mutation) => {
    if (mutation.attributeName === 'data-md-color-scheme') {
      location.reload();
    }
  });
});

const body = document.body;
observer.observe(body, { attributes: true });
