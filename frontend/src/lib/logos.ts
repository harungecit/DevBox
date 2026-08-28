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
