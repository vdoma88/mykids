#!/usr/bin/env node
'use strict';
/**
 * Собирает content/packs/index.json — перечень пакетов для раннера и админки.
 * Без него клиенту пришлось бы уметь листать каталог, чего браузер не умеет.
 *
 *   node bin/build-index.js
 */
const fs = require('fs');
const path = require('path');

const REPO = path.resolve(__dirname, '../../..');
const PACKS = path.join(REPO, 'content/packs');

function main() {
  const packs = fs
    .readdirSync(PACKS, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .sort()
    .map((id) => {
      const m = JSON.parse(fs.readFileSync(path.join(PACKS, id, 'pack.json'), 'utf8'));
      return {
        id: m.id,
        title: m.title,
        subject: m.subject,
        version: m.version,
        itemCount: m.items.length,
        ...(m.description ? { description: m.description } : {}),
      };
    });

  const out = path.join(PACKS, 'index.json');
  fs.writeFileSync(out, JSON.stringify({ packs }, null, 2) + '\n');
  console.log(`index.json: пакетов ${packs.length}, заданий ${packs.reduce((s, p) => s + p.itemCount, 0)}`);
}

main();
