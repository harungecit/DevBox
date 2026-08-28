(function () {
  var KEY = 'devbox-lang';
  function pick() {
    try { var s = localStorage.getItem(KEY); if (s) return s; } catch (e) {}
    return (navigator.language || '').toLowerCase().indexOf('tr') === 0 ? 'tr' : 'en';
  }
  function apply(lang) {
    document.documentElement.setAttribute('lang', lang);
    document.querySelectorAll('.lang button').forEach(function (b) {
      b.classList.toggle('on', b.getAttribute('data-set') === lang);
    });
    try { localStorage.setItem(KEY, lang); } catch (e) {}
  }
  document.addEventListener('DOMContentLoaded', function () {
    apply(pick());
    document.querySelectorAll('.lang button').forEach(function (b) {
      b.addEventListener('click', function () { apply(b.getAttribute('data-set')); });
    });
    // Resolve the latest release asset for the download button.
    var btn = document.getElementById('dl-win');
    var ver = document.getElementById('dl-ver');
    if (btn) {
      fetch('https://api.github.com/repos/harungecit/DevBox/releases/latest')
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (rel) {
          if (!rel) return;
          var asset = (rel.assets || []).find(function (a) { return /^DevBox-Setup-.*windows-amd64\.exe$/i.test(a.name); });
          if (asset) btn.href = asset.browser_download_url;
          if (ver) ver.textContent = rel.tag_name;
        }).catch(function () {});
    }
  });
})();
