// SVG logos for services and runtimes
// Icons imported from assets/images/icons/ via Vite ?raw imports.
// sanitize() prefixes all IDs and CSS classes to prevent conflicts
// when multiple SVGs are rendered inline on the same page.

// @ts-ignore
import nginxSvg from '../assets/images/icons/nginx-icon.svg?raw';
// @ts-ignore
import apacheSvg from '../assets/images/icons/apache-icon.svg?raw';
// @ts-ignore
import caddySvg from '../assets/images/icons/caddyserver-icon.svg?raw';
// @ts-ignore
import postgresSvg from '../assets/images/icons/postgresql-icon.svg?raw';
// @ts-ignore
import mysqlSvg from '../assets/images/icons/mysql-icon.svg?raw';
// @ts-ignore
import mariadbSvg from '../assets/images/icons/mariadb-icon.svg?raw';
// @ts-ignore
import mongodbSvg from '../assets/images/icons/mongodb-icon.svg?raw';
// @ts-ignore
import redisSvg from '../assets/images/icons/redis-icon.svg?raw';
// @ts-ignore
import valkeySvg from '../assets/images/icons/valkey-icon.svg?raw';
// @ts-ignore
import frankenphpSvg from '../assets/images/icons/frankenphp-icon.svg?raw';
// @ts-ignore
import mailpitSvg from '../assets/images/icons/mailpit-icon.svg?raw';
// @ts-ignore
import goSvg from '../assets/images/icons/golang-icon.svg?raw';
// @ts-ignore
import nodeSvg from '../assets/images/icons/nodejs-icon.svg?raw';
// @ts-ignore
import phpSvg from '../assets/images/icons/php-icon.svg?raw';
// @ts-ignore
import pythonSvg from '../assets/images/icons/python-icon.svg?raw';
// @ts-ignore
import rustSvg from '../assets/images/icons/rust-lang-icon.svg?raw';

// Plugin runtime logos (simple-icons, CC0) keyed by vfox plugin id.
// @ts-ignore
import plugin_javaSvg from '../assets/images/icons/plugin-openjdk.svg?raw';
// @ts-ignore
import plugin_dotnetSvg from '../assets/images/icons/plugin-dotnet.svg?raw';
// @ts-ignore
import plugin_rubySvg from '../assets/images/icons/plugin-ruby.svg?raw';
// @ts-ignore
import plugin_denoSvg from '../assets/images/icons/plugin-deno.svg?raw';
// @ts-ignore
import plugin_bunSvg from '../assets/images/icons/plugin-bun.svg?raw';
// @ts-ignore
import plugin_dartSvg from '../assets/images/icons/plugin-dart.svg?raw';
// @ts-ignore
import plugin_flutterSvg from '../assets/images/icons/plugin-flutter.svg?raw';
// @ts-ignore
import plugin_kotlinSvg from '../assets/images/icons/plugin-kotlin.svg?raw';
// @ts-ignore
import plugin_zigSvg from '../assets/images/icons/plugin-zig.svg?raw';
// @ts-ignore
import plugin_elixirSvg from '../assets/images/icons/plugin-elixir.svg?raw';
// @ts-ignore
import plugin_erlangSvg from '../assets/images/icons/plugin-erlang.svg?raw';
// @ts-ignore
import plugin_juliaSvg from '../assets/images/icons/plugin-julia.svg?raw';
// @ts-ignore
import plugin_gradleSvg from '../assets/images/icons/plugin-gradle.svg?raw';
// @ts-ignore
import plugin_mavenSvg from '../assets/images/icons/plugin-apachemaven.svg?raw';
// @ts-ignore
import plugin_terraformSvg from '../assets/images/icons/plugin-terraform.svg?raw';
// @ts-ignore
import plugin_kubectlSvg from '../assets/images/icons/plugin-kubernetes.svg?raw';
// @ts-ignore
import plugin_cmakeSvg from '../assets/images/icons/plugin-cmake.svg?raw';
// @ts-ignore
import plugin_luaSvg from '../assets/images/icons/plugin-lua.svg?raw';
// @ts-ignore
import plugin_crystalSvg from '../assets/images/icons/plugin-crystal.svg?raw';
// @ts-ignore
import plugin_scalaSvg from '../assets/images/icons/plugin-scala.svg?raw';
// @ts-ignore
import plugin_groovySvg from '../assets/images/icons/plugin-apachegroovy.svg?raw';
// @ts-ignore
import plugin_clangSvg from '../assets/images/icons/plugin-llvm.svg?raw';
// @ts-ignore
import plugin_vlangSvg from '../assets/images/icons/plugin-v.svg?raw';
// @ts-ignore
import plugin_typstSvg from '../assets/images/icons/plugin-typst.svg?raw';
// @ts-ignore
import plugin_vagrantSvg from '../assets/images/icons/plugin-vagrant.svg?raw';
// @ts-ignore
import plugin_etcdSvg from '../assets/images/icons/plugin-etcd.svg?raw';
// @ts-ignore
import plugin_tomcatSvg from '../assets/images/icons/plugin-apachetomcat.svg?raw';
// @ts-ignore
import plugin_helmSvg from '../assets/images/icons/plugin-helm.svg?raw';
// @ts-ignore
import plugin_swiftSvg from '../assets/images/icons/plugin-swift.svg?raw';
// @ts-ignore
import plugin_nimSvg from '../assets/images/icons/plugin-nim.svg?raw';
// @ts-ignore
import plugin_haskellSvg from '../assets/images/icons/plugin-haskell.svg?raw';
// @ts-ignore
import plugin_perlSvg from '../assets/images/icons/plugin-perl.svg?raw';
// @ts-ignore
import plugin_rSvg from '../assets/images/icons/plugin-r.svg?raw';
// @ts-ignore
import plugin_ocamlSvg from '../assets/images/icons/plugin-ocaml.svg?raw';
// @ts-ignore
import plugin_gleamSvg from '../assets/images/icons/plugin-gleam.svg?raw';
// @ts-ignore
import plugin_odinSvg from '../assets/images/icons/plugin-odin.svg?raw';

/**
 * Sanitize an SVG string for safe inline rendering:
 * - Strips XML declarations and comments
 * - Removes fixed width/height (container controls sizing)
 * - Prefixes all id and class names to avoid DOM conflicts
 */
function sanitize(svg: string, prefix: string): string {
  let s = svg
    .replace(/<\?xml[^?]*\?>/g, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .trim();

  // Replace fixed width/height with 100% so SVG fills its container
  s = s.replace(/(<svg[^>]*?)\s+width="[^"]*"/g, '$1');
  s = s.replace(/(<svg[^>]*?)\s+height="[^"]*"/g, '$1');
  s = s.replace(/<svg/, '<svg width="100%" height="100%"');

  // Collect all element IDs
  const ids: string[] = [];
  s.replace(/\bid="([^"]+)"/g, (_, id) => { ids.push(id); return ''; });

  // Prefix each ID and update all references (url(#id), xlink:href="#id", href="#id")
  for (const id of ids) {
    const pid = `${prefix}_${id}`;
    s = s.split(`id="${id}"`).join(`id="${pid}"`);
    s = s.split(`"#${id}"`).join(`"#${pid}"`);
    s = s.split(`(#${id})`).join(`(#${pid})`);
  }

  // Handle CSS class conflicts in <style> blocks
  const styleMatch = s.match(/<style[^>]*>([\s\S]*?)<\/style>/);
  if (styleMatch) {
    const classNames: string[] = [];
    let style = styleMatch[1].replace(/<!\[CDATA\[/g, '').replace(/\]\]>/g, '');
    style.replace(/\.([A-Za-z_][A-Za-z0-9_-]*)\s*\{/g, (_, cls) => {
      if (!classNames.includes(cls)) classNames.push(cls);
      return '';
    });

    for (const cls of classNames) {
      const pcls = `${prefix}_${cls}`;
      style = style.split(`.${cls}`).join(`.${pcls}`);
    }

    // Replace style block
    s = s.replace(/<style[^>]*>[\s\S]*?<\/style>/, `<style>${style}</style>`);

    // Prefix class names in class attributes
    if (classNames.length > 0) {
      s = s.replace(/\bclass="([^"]*)"/g, (_, classes) => {
        const updated = (classes as string).split(/\s+/).map((c: string) => {
          return classNames.includes(c) ? `${prefix}_${c}` : c;
        }).join(' ');
        return `class="${updated}"`;
      });
    }
  }

  // Handle inline style class (.st0 etc. in Go icon)
  const inlineStyleMatch = s.match(/<style[^>]*>\.(st\d+)\{([^}]+)\}<\/style>/);
  if (inlineStyleMatch) {
    const cls = inlineStyleMatch[1];
    const props = inlineStyleMatch[2];
    // Convert class-based styles to inline style attributes
    s = s.replace(/<style[^>]*>[^<]*<\/style>/, '');
    s = s.replace(new RegExp(`class="${cls}"`, 'g'), `style="${props}"`);
  }

  return s;
}

export const serviceLogos: Record<string, string> = {
  nginx: sanitize(nginxSvg, 'nx'),
  apache: sanitize(apacheSvg, 'ap'),
  caddy: sanitize(caddySvg, 'cd'),
  frankenphp: sanitize(frankenphpSvg, 'fp'),
  postgres: sanitize(postgresSvg, 'pg'),
  mysql: sanitize(mysqlSvg, 'my'),
  mariadb: sanitize(mariadbSvg, 'mdb'),
  mongodb: sanitize(mongodbSvg, 'mgo'),
  redis: sanitize(redisSvg, 'rd'),
  valkey: sanitize(valkeySvg, 'vk'),
  mailpit: sanitize(mailpitSvg, 'mp'),
};

export const runtimeLogos: Record<string, string> = {
  go: sanitize(goSvg, 'go'),
  node: sanitize(nodeSvg, 'nd'),
  php: sanitize(phpSvg, 'php'),
  python: sanitize(pythonSvg, 'py'),
  rust: sanitize(rustSvg, 'rs'),
};

// Logos for popular vfox plugin runtimes. Keys are vfox plugin ids.
export const pluginLogos: Record<string, string> = {
  java: sanitize(plugin_javaSvg, 'pl_java'),
  dotnet: sanitize(plugin_dotnetSvg, 'pl_dotnet'),
  ruby: sanitize(plugin_rubySvg, 'pl_ruby'),
  deno: sanitize(plugin_denoSvg, 'pl_deno'),
  bun: sanitize(plugin_bunSvg, 'pl_bun'),
  dart: sanitize(plugin_dartSvg, 'pl_dart'),
  flutter: sanitize(plugin_flutterSvg, 'pl_flutter'),
  kotlin: sanitize(plugin_kotlinSvg, 'pl_kotlin'),
  zig: sanitize(plugin_zigSvg, 'pl_zig'),
  elixir: sanitize(plugin_elixirSvg, 'pl_elixir'),
  erlang: sanitize(plugin_erlangSvg, 'pl_erlang'),
  julia: sanitize(plugin_juliaSvg, 'pl_julia'),
  gradle: sanitize(plugin_gradleSvg, 'pl_gradle'),
  maven: sanitize(plugin_mavenSvg, 'pl_maven'),
  terraform: sanitize(plugin_terraformSvg, 'pl_terraform'),
  kubectl: sanitize(plugin_kubectlSvg, 'pl_kubectl'),
  cmake: sanitize(plugin_cmakeSvg, 'pl_cmake'),
  lua: sanitize(plugin_luaSvg, 'pl_lua'),
  crystal: sanitize(plugin_crystalSvg, 'pl_crystal'),
  scala: sanitize(plugin_scalaSvg, 'pl_scala'),
  groovy: sanitize(plugin_groovySvg, 'pl_groovy'),
  clang: sanitize(plugin_clangSvg, 'pl_clang'),
  vlang: sanitize(plugin_vlangSvg, 'pl_vlang'),
  typst: sanitize(plugin_typstSvg, 'pl_typst'),
  vagrant: sanitize(plugin_vagrantSvg, 'pl_vagrant'),
  etcd: sanitize(plugin_etcdSvg, 'pl_etcd'),
  tomcat: sanitize(plugin_tomcatSvg, 'pl_tomcat'),
  helm: sanitize(plugin_helmSvg, 'pl_helm'),
  swift: sanitize(plugin_swiftSvg, 'pl_swift'),
  nim: sanitize(plugin_nimSvg, 'pl_nim'),
  haskell: sanitize(plugin_haskellSvg, 'pl_haskell'),
  perl: sanitize(plugin_perlSvg, 'pl_perl'),
  r: sanitize(plugin_rSvg, 'pl_r'),
  ocaml: sanitize(plugin_ocamlSvg, 'pl_ocaml'),
  gleam: sanitize(plugin_gleamSvg, 'pl_gleam'),
  odin: sanitize(plugin_odinSvg, 'pl_odin'),
};

// Deterministic fallback badge (rounded square, initial letter) for runtimes
// without a bundled logo — hue derived from the name so it stays stable.
function initialBadge(label: string): string {
  let hash = 0;
  for (let i = 0; i < label.length; i++) hash = (hash * 31 + label.charCodeAt(i)) >>> 0;
  const hue = hash % 360;
  const initial = (label.trim().charAt(0) || '?').toUpperCase().replace(/[<>&"']/g, '');
  return `<svg viewBox="0 0 32 32" width="100%" height="100%" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="7" fill="hsl(${hue} 55% 45%)"/><text x="16" y="21.5" text-anchor="middle" font-family="system-ui, -apple-system, Segoe UI, sans-serif" font-size="16" font-weight="700" fill="#fff">${initial}</text></svg>`;
}

// runtimeLogo returns the best logo for a runtime id: built-in, bundled plugin
// logo, or a generated badge. Always renders something.
export function runtimeLogo(name: string, displayName?: string): string {
  return runtimeLogos[name] || pluginLogos[name] || initialBadge(displayName || name);
}
