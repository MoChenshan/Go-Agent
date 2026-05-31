package main

const adminSidebarScrollCSS = `      overscroll-behavior: contain;
      scrollbar-gutter: stable;`

const adminSidebarInjectedStyleHTML = `    .sidebar {
      overflow-y: auto;
` + adminSidebarScrollCSS + `
    }
    @media (max-width: 760px) {
      .sidebar {
        overflow: visible;
      }
    }`

const adminSidebarRevealScriptHTML = `  <script>
    (function() {
      const pendingScrollKey = "openclaw.admin.pendingScroll";
      const pendingScrollMaxAgeMS = 30000;
      const sidebar = document.querySelector(".sidebar");
      const viewportPadding = 16;

      function readPendingScroll() {
        try {
          const raw = window.sessionStorage.getItem(pendingScrollKey);
          if (!raw) return null;
          window.sessionStorage.removeItem(pendingScrollKey);
          const value = JSON.parse(raw);
          if (!value || typeof value !== "object") return null;
          if (Date.now() - value.savedAt > pendingScrollMaxAgeMS) {
            return null;
          }
          if (
            typeof value.targetPath === "string" &&
            value.targetPath !== window.location.pathname
          ) {
            return null;
          }
          return value;
        } catch (err) {
          return null;
        }
      }

      function savePendingScroll(targetPath) {
        try {
          window.sessionStorage.setItem(pendingScrollKey, JSON.stringify({
            savedAt: Date.now(),
            targetPath: targetPath,
            sidebarTop: sidebar ? sidebar.scrollTop : 0
          }));
        } catch (err) {}
      }

      function revealActiveLink() {
        const activeLink = sidebar &&
          sidebar.querySelector(".sidebar-link.active");
        if (!activeLink) {
          return;
        }
        const activeRect = activeLink.getBoundingClientRect();
        if (sidebar.scrollHeight <= sidebar.clientHeight) {
          const topEdge = viewportPadding;
          const bottomEdge = window.innerHeight - viewportPadding;
          if (activeRect.top < topEdge) {
            window.scrollBy(0, activeRect.top - topEdge);
            return;
          }
          if (activeRect.bottom > bottomEdge) {
            window.scrollBy(0, activeRect.bottom - bottomEdge);
          }
          return;
        }
        const sidebarRect = sidebar.getBoundingClientRect();
        const topEdge = sidebarRect.top + viewportPadding;
        const bottomEdge = sidebarRect.bottom - viewportPadding;
        if (activeRect.top < topEdge) {
          sidebar.scrollTop -= topEdge - activeRect.top;
          return;
        }
        if (activeRect.bottom > bottomEdge) {
          sidebar.scrollTop += activeRect.bottom - bottomEdge;
        }
      }

      document.querySelectorAll(".sidebar-link").forEach(function(link) {
        link.addEventListener("click", function(evt) {
          if (
            evt.defaultPrevented ||
            evt.button !== 0 ||
            evt.metaKey ||
            evt.ctrlKey ||
            evt.shiftKey ||
            evt.altKey
          ) {
            return;
          }
          const targetURL = new URL(link.href, window.location.href);
          savePendingScroll(targetURL.pathname);
        });
      });

      const pendingScroll = readPendingScroll();
      if (pendingScroll) {
        if (sidebar && Number.isFinite(pendingScroll.sidebarTop)) {
          sidebar.scrollTop = pendingScroll.sidebarTop;
        }
      } else {
        revealActiveLink();
      }
    })();
  </script>`
