#!/usr/bin/env node
'use strict';
/**
 * Переносит контент legacy-приложения UKids Home в пакеты заданий.
 *
 *   node bin/import-ukids.js [--out <dir>] [--src <html>]
 *
 * Источник — объявления WEEKS и BONUS_EXERCISES внутри legacy/ukids-app.html.
 * Скрипт извлекает их из текста, а не исполняет приложение: оно завязано на DOM.
 *
 * Из каждой недели получается пакет: интерактивное упражнение, рефлексия и
 * пункты выходного чек-листа как задания с подтверждением родителя. Утверждения
 * для шкалы самооценки взяты из таблицы ниже — они не выводятся из legacy-данных.
 *
 * Скрипт идемпотентен: повторный запуск перезаписывает сгенерированные пакеты.
 */
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const REPO = path.resolve(__dirname, '../../..');
const DEFAULT_SRC = path.join(REPO, 'legacy/ukids-app.html');
const DEFAULT_OUT = path.join(REPO, 'content/packs');

/** Виджеты legacy-приложения → идентификаторы в task-runner. */
const WIDGETS = {
  schulte: 'schulte', smart: 'smart-goal', eisenhower: 'eisenhower',
  breathing: 'breathing', facts: 'facts-vs-opinion', memory: 'memory-pairs',
  istatement: 'i-statement', conflict: 'conflict-cases', mindmap: 'mindmap',
  speech: 'speech-timer', roles: 'team-roles', certificate: 'certificate',
  values: 'values-map', vision: 'vision-board', habit: 'habit-tracker',
  digital: 'digital-balance', decision: 'decision-matrix',
  negotiation: 'negotiation-winwin', pitch: 'pitch-30s'
};

/** Короткое имя темы недели для идентификатора пакета. */
const SLUGS = {
  1: 'attention', 2: 'motivation', 3: 'time', 4: 'emotions', 5: 'critical-thinking',
  6: 'memory', 7: 'boundaries', 8: 'conflicts', 9: 'creativity', 10: 'speaking',
  11: 'teamwork', 12: 'final'
};

/**
 * Утверждения для шкалы самооценки — по одному на неделю.
 * Курируются вручную: в legacy-данных их нет.
 */
const LIKERT = {
  1: ['Мне легко сосредоточиться на задании, даже если рядом лежит телефон.', 'attention_control'],
  2: ['Я понимаю, зачем лично мне то, чем я занимаюсь в школе.', 'intrinsic_motivation'],
  3: ['Я замечаю, на что уходит моё время в течение дня.', 'time_awareness'],
  4: ['Я могу назвать словами, что чувствую, когда злюсь или расстроен(а).', 'emotional_awareness'],
  5: ['Прежде чем поверить чему-то в интернете, я думаю, факт это или мнение.', 'critical_thinking'],
  6: ['У меня есть свои приёмы, чтобы запоминать новое.', 'learning_strategies'],
  7: ['Мне удаётся сказать «нет», когда я не хочу соглашаться.', 'boundaries'],
  8: ['В споре я могу говорить спокойно, даже если не согласен(на).', 'conflict_skills'],
  9: ['Я не боюсь предлагать необычные идеи, даже если они могут не подойти.', 'creativity'],
  10: ['Я справляюсь с волнением, когда нужно говорить перед другими.', 'public_speaking'],
  11: ['Я понимаю, какая роль в команде мне подходит.', 'teamwork'],
  12: ['За последние недели я стал(а) лучше понимать себя.', 'self_reflection']
};

/** Достаёт объявление массива из текста файла и вычисляет его в песочнице. */
function extractArray(source, name) {
  const start = source.indexOf(`const ${name} = [`);
  if (start === -1) throw new Error(`не найдено объявление ${name}`);
  const open = source.indexOf('[', start);

  let depth = 0, inStr = null, end = -1;
  for (let i = open; i < source.length; i++) {
    const ch = source[i];
    if (inStr) {
      if (ch === '\\') i++;
      else if (ch === inStr) inStr = null;
      continue;
    }
    if (ch === "'" || ch === '"' || ch === '`') { inStr = ch; continue; }
    if (ch === '[') depth++;
    else if (ch === ']' && --depth === 0) { end = i + 1; break; }
  }
  if (end === -1) throw new Error(`не удалось найти конец массива ${name}`);

  return vm.runInNewContext('(' + source.slice(open, end) + ')', Object.create(null));
}

function widgetFor(key, name) {
  const widget = WIDGETS[key];
  if (!widget) throw new Error(`нет сопоставления виджета для "${key}" (${name})`);
  return widget;
}

function weekPack(week) {
  const num = String(week.n).padStart(2, '0');
  const slug = SLUGS[week.n];
  if (!slug) throw new Error(`нет slug для недели ${week.n}`);
  const id = `ru.mykids.psychology.week${num}.${slug}`;
  const items = [];

  items.push({
    id: `psy-w${num}-exercise`,
    type: 'interactive',
    difficulty: 2,
    stem: week.wd,
    widget: widgetFor(week.ex, `неделя ${week.n}`),
    completionRule: { minDurationSec: 30 },
    credits: 3,
    hints: week.hint11 ? [week.hint11] : undefined,
    topics: [week.wd]
  });

  items.push({
    id: `psy-w${num}-reflection`,
    type: 'reflection',
    stem: `${week.wdDesc} Что из этого получилось попробовать на этой неделе?`,
    minChars: 60,
    credits: 2
  });

  const [statement, dimension] = LIKERT[week.n];
  items.push({
    id: `psy-w${num}-likert`,
    type: 'likert',
    stem: statement,
    scale: { min: 1, max: 5, minLabel: 'Совсем не про меня', maxLabel: 'Точно про меня' },
    dimension,
    credits: 1
  });

  (week.weChecklist || []).forEach((task, i) => {
    items.push({
      id: `psy-w${num}-practice-${i + 1}`,
      type: 'parent_verified',
      stem: task,
      verificationPrompt: `Пункт практикума выполнен: «${task}»?`,
      credits: 3,
      cooldownHours: 168
    });
  });

  return {
    manifest: {
      id,
      schemaVersion: 1,
      version: '1.0.0',
      title: `Неделя ${week.n}. ${week.wd}`,
      description: `${week.wdDesc} Практикум выходного дня: ${week.weDesc}`,
      subject: 'psychology',
      gradeRange: [11, 13],
      locale: 'ru-RU',
      author: 'UKids Home',
      license: 'CC-BY-NC-4.0',
      tags: ['психология', `неделя ${week.n}`],
      reward: { creditsPerCorrect: 2, bonusOnPerfect: 3, dailyCreditCap: 20 },
      delivery: {
        itemsPerSession: items.length,
        selection: 'sequential',
        passThreshold: 0.5,
        cooldownHoursPerItem: 168
      },
      items: items.map((_, i) => `items/${String(i + 1).padStart(3, '0')}.json`)
    },
    items
  };
}

function bonusPack(bonusExercises) {
  const items = bonusExercises.map((b, i) => ({
    id: `psy-bonus-${String(i + 1).padStart(2, '0')}-${WIDGETS[b.key]}`,
    type: 'interactive',
    difficulty: 2,
    stem: `${b.title}. ${b.desc}`,
    widget: widgetFor(b.key, b.title),
    completionRule: { minDurationSec: 30 },
    credits: 4,
    topics: [b.title]
  }));

  return {
    manifest: {
      id: 'ru.mykids.psychology.bonus.skills',
      schemaVersion: 1,
      version: '1.0.0',
      title: 'Дополнительные навыки',
      description: 'Семь коротких инструментов сверх основной программы: ценности, мечты, привычки, экранный баланс, решения, переговоры и самопрезентация.',
      subject: 'psychology',
      gradeRange: [11, 13],
      locale: 'ru-RU',
      author: 'UKids Home',
      license: 'CC-BY-NC-4.0',
      tags: ['психология', 'доп. навыки'],
      reward: { creditsPerCorrect: 4, bonusOnPerfect: 5, dailyCreditCap: 24 },
      delivery: {
        itemsPerSession: 1,
        selection: 'random',
        passThreshold: 0.5,
        cooldownHoursPerItem: 168
      },
      items: items.map((_, i) => `items/${String(i + 1).padStart(3, '0')}.json`)
    },
    items
  };
}

/** Советы родителям и темы встреч — не задания ребёнка, но терять их нельзя. */
function parentTrack(weeks, meetings) {
  return {
    source: 'UKids Home',
    weeklyTips: weeks.map(w => ({ week: w.n, tip: w.parentTip })),
    meetings: meetings.map((m, i) => ({
      index: i + 1,
      weeks: [i * 2 + 1, i * 2 + 2],
      title: m.t,
      summary: m.tip
    }))
  };
}

function writePack(outDir, pack) {
  const dir = path.join(outDir, pack.manifest.id);
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(path.join(dir, 'items'), { recursive: true });

  fs.writeFileSync(path.join(dir, 'pack.json'), JSON.stringify(pack.manifest, null, 2) + '\n');
  pack.items.forEach((item, i) => {
    // undefined-поля не должны попадать в JSON пустыми ключами
    const clean = JSON.parse(JSON.stringify(item));
    fs.writeFileSync(
      path.join(dir, 'items', `${String(i + 1).padStart(3, '0')}.json`),
      JSON.stringify(clean, null, 2) + '\n'
    );
  });
  return pack.items.length;
}

function main() {
  const argv = process.argv.slice(2);
  const src = argv.includes('--src') ? argv[argv.indexOf('--src') + 1] : DEFAULT_SRC;
  const out = argv.includes('--out') ? argv[argv.indexOf('--out') + 1] : DEFAULT_OUT;

  const source = fs.readFileSync(src, 'utf8');
  const weeks = extractArray(source, 'WEEKS');
  const bonus = extractArray(source, 'BONUS_EXERCISES');
  const meetings = extractArray(source, 'PARENT_MEETINGS');
  console.log(`Прочитано: недель ${weeks.length}, доп. навыков ${bonus.length}, встреч ${meetings.length}`);

  fs.mkdirSync(out, { recursive: true });
  let total = 0;
  for (const week of weeks) {
    const pack = weekPack(week);
    const count = writePack(out, pack);
    total += count;
    console.log(`  ${pack.manifest.id} — заданий: ${count}`);
  }

  const bp = bonusPack(bonus);
  total += writePack(out, bp);
  console.log(`  ${bp.manifest.id} — заданий: ${bp.items.length}`);

  const trackDir = path.join(REPO, 'content/parent-track');
  fs.mkdirSync(trackDir, { recursive: true });
  fs.writeFileSync(
    path.join(trackDir, 'ukids.json'),
    JSON.stringify(parentTrack(weeks, meetings), null, 2) + '\n'
  );
  console.log('  content/parent-track/ukids.json — советы родителям и темы встреч');

  console.log(`\nИтого пакетов: ${weeks.length + 1}, заданий: ${total}`);
}

main();
