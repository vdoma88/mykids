export * from './answer.js';
export * from './grade.js';
export * from './session.js';
// Загрузчик и рендерер нужны не только странице локального прогона: тем же
// кодом задания решает ребёнок в своём интерфейсе. Две копии разошлись бы.
export * from './load.js';
export * from './ui/render.js';
