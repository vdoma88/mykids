#!/usr/bin/env node
'use strict';
/**
 * Валидатор пакетов заданий MyKids.
 *
 *   node bin/validate.js <путь-к-пакету> [...]
 *
 * Проверяет структуру по JSON Schema плюс правила, которые схемой не выражаются:
 * уникальность id, отсутствие «сирот» в items/, корректность правильных ответов,
 * разумность антифарм-лимитов и разрешимость ссылок на assets/.
 */
const fs = require('fs');
const path = require('path');
const Ajv = require('ajv/dist/2020');

const SCHEMA_DIR = path.resolve(__dirname, '../../../docs/content-format');
const SCORED_TYPES = new Set([
  'single_choice', 'multi_choice', 'numeric', 'short_text',
  'ordering', 'matching', 'cloze', 'code'
]);

const ajv = new Ajv({ allErrors: true, strict: false });
const validatePack = ajv.compile(readJson(path.join(SCHEMA_DIR, 'task-pack.schema.json')));
const validateItem = ajv.compile(readJson(path.join(SCHEMA_DIR, 'task-item.schema.json')));

function readJson(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

function formatAjvErrors(errors) {
  return errors.map(e => `${e.instancePath || '/'} ${e.message}`);
}

/** Проверяет один пакет, возвращает { errors, warnings, itemIds }. */
function checkPack(packDir) {
  const errors = [];
  const warnings = [];
  const itemIds = new Map();

  const manifestPath = path.join(packDir, 'pack.json');
  if (!fs.existsSync(manifestPath)) {
    return { errors: ['нет pack.json'], warnings, itemIds };
  }

  let manifest;
  try {
    manifest = readJson(manifestPath);
  } catch (e) {
    return { errors: [`pack.json — некорректный JSON: ${e.message}`], warnings, itemIds };
  }

  if (!validatePack(manifest)) {
    errors.push(...formatAjvErrors(validatePack.errors).map(m => `pack.json: ${m}`));
  }

  if (manifest.reward && manifest.reward.dailyCreditCap > 100) {
    warnings.push(
      `pack.json: dailyCreditCap = ${manifest.reward.dailyCreditCap} — подозрительно много, ` +
      `проверьте, что пакет нельзя фармить`
    );
  }

  // Каждый файл из items[] существует и валиден.
  const declared = new Set(manifest.items || []);
  for (const rel of declared) {
    const itemPath = path.join(packDir, rel);
    if (!fs.existsSync(itemPath)) {
      errors.push(`${rel}: файл объявлен в манифесте, но отсутствует`);
      continue;
    }

    let item;
    try {
      item = readJson(itemPath);
    } catch (e) {
      errors.push(`${rel}: некорректный JSON — ${e.message}`);
      continue;
    }

    if (!validateItem(item)) {
      errors.push(...formatAjvErrors(validateItem.errors).map(m => `${rel}: ${m}`));
      continue;
    }

    if (itemIds.has(item.id)) {
      errors.push(`${rel}: id "${item.id}" уже занят файлом ${itemIds.get(item.id)}`);
    } else {
      itemIds.set(item.id, rel);
    }

    errors.push(...checkItemSemantics(item, rel, packDir));
  }

  // Файлы в items/, забытые в манифесте, иначе тихо не попадут в выдачу.
  const itemsDir = path.join(packDir, 'items');
  if (fs.existsSync(itemsDir)) {
    for (const file of fs.readdirSync(itemsDir)) {
      if (!file.endsWith('.json')) continue;
      const rel = `items/${file}`;
      if (!declared.has(rel)) {
        warnings.push(`${rel}: файл есть на диске, но не перечислен в pack.json — не будет показан`);
      }
    }
  }

  return { errors, warnings, itemIds };
}

/** Правила корректности ответов, которые JSON Schema не выражает. */
function checkItemSemantics(item, rel, packDir) {
  const errors = [];

  if (item.type === 'single_choice') {
    const correct = item.options.filter(o => o.correct).length;
    if (correct !== 1) {
      errors.push(`${rel}: single_choice должен иметь ровно один верный вариант, найдено ${correct}`);
    }
  }

  if (item.type === 'multi_choice') {
    const correct = item.options.filter(o => o.correct).length;
    if (correct < 1) {
      errors.push(`${rel}: multi_choice должен иметь хотя бы один верный вариант`);
    }
  }

  if (item.type === 'numeric') {
    const { tolerance, relativeTolerance } = item.answer;
    if (tolerance === undefined && relativeTolerance === undefined) {
      errors.push(
        `${rel}: у numeric не задан допуск (tolerance или relativeTolerance) — ` +
        `ответ будет сверяться точно, что почти всегда ошибка`
      );
    }
  }

  if (item.type === 'short_text') {
    const { accepted, pattern } = item.answer || {};
    if (!accepted && !pattern) {
      errors.push(`${rel}: у short_text нужен либо accepted, либо pattern`);
    }
    if (pattern) {
      try {
        new RegExp(pattern);
      } catch (e) {
        errors.push(`${rel}: некорректный regex в answer.pattern — ${e.message}`);
      }
    }
  }

  if (item.type === 'matching') {
    const rights = item.pairs.map(p => p.right);
    if (new Set(rights).size !== rights.length) {
      errors.push(`${rel}: правые части в matching должны быть уникальны, иначе задание неоднозначно`);
    }
  }

  if (item.type === 'cloze') {
    const ids = item.blanks.map(b => b.id);
    if (new Set(ids).size !== ids.length) {
      errors.push(`${rel}: id пропусков в cloze должны быть уникальны`);
    }
    for (const b of item.blanks) {
      if (!item.stem.includes(`{{${b.id}}}`)) {
        errors.push(`${rel}: пропуск "${b.id}" не найден в тексте задания (ожидается {{${b.id}}})`);
      }
    }
  }

  if (SCORED_TYPES.has(item.type) && item.credits === 0) {
    errors.push(`${rel}: оцениваемое задание с credits = 0 никогда ничего не начислит`);
  }

  if (item.media) {
    const assetPath = path.join(packDir, item.media);
    if (!fs.existsSync(assetPath)) {
      errors.push(`${rel}: ресурс ${item.media} не найден`);
    }
  }

  return errors;
}

function main() {
  const targets = process.argv.slice(2);
  if (targets.length === 0) {
    console.error('Использование: validate.js <путь-к-пакету> [...]');
    process.exit(2);
  }

  const globalIds = new Map();
  let totalErrors = 0;
  let totalWarnings = 0;

  for (const target of targets) {
    const packDir = path.resolve(target);
    const name = path.basename(packDir);
    const { errors, warnings, itemIds } = checkPack(packDir);

    // id заданий уникальны не только внутри пакета, но и по всему репозиторию:
    // журнал попыток ссылается на них глобально.
    for (const [id, rel] of itemIds) {
      if (globalIds.has(id)) {
        errors.push(`${rel}: id "${id}" уже использован в пакете ${globalIds.get(id)}`);
      } else {
        globalIds.set(id, name);
      }
    }

    for (const w of warnings) console.log(`  warn  ${name}: ${w}`);
    for (const e of errors) console.log(`  ERROR ${name}: ${e}`);
    if (errors.length === 0) {
      console.log(`  ok    ${name} — заданий: ${itemIds.size}`);
    }

    totalErrors += errors.length;
    totalWarnings += warnings.length;
  }

  console.log(
    `\nПакетов: ${targets.length}, заданий: ${globalIds.size}, ` +
    `ошибок: ${totalErrors}, предупреждений: ${totalWarnings}`
  );
  process.exit(totalErrors > 0 ? 1 : 0);
}

main();
