// Tiny bit of vanilla JS to replace inline onclick/onsubmit handlers, which
// a strict Content-Security-Policy (script-src 'self', no 'unsafe-inline')
// otherwise blocks. Event delegation on document so it keeps working for
// anything htmx swaps in later, without needing to re-bind listeners.

document.addEventListener("click", function (e) {
  if (e.target.closest('[data-action="print"]')) {
    window.print();
  }
});

document.addEventListener(
  "submit",
  function (e) {
    var message = e.target.getAttribute("data-confirm");
    if (message && !window.confirm(message)) {
      e.preventDefault();
    }
  },
  true
);
