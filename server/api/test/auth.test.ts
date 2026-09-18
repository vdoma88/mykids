import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import * as OTPAuth from 'otpauth';
import { AuthError, AuthService } from '../src/services/auth.service.js';
import { prisma, resetDb, seedFamily } from './helpers.js';

const auth = new AuthService(prisma);

beforeEach(resetDb);
afterAll(async () => { await prisma.$disconnect(); });

const creds = { familyName: 'Семья', email: 'parent@example.com', password: 'очень-длинный-пароль' };

describe('регистрация и вход', () => {
  it('регистрирует владельца семьи', async () => {
    const { guardianId } = await auth.register(creds);
    const g = await prisma.guardian.findUniqueOrThrow({ where: { id: guardianId } });
    expect(g.role).toBe('owner');
    expect(g.passwordHash).not.toContain(creds.password); // пароль не лежит в открытом виде
  });

  it('отвергает короткий пароль и занятый адрес', async () => {
    await expect(auth.register({ ...creds, password: 'коротко' }))
      .rejects.toMatchObject({ code: 'weak_password' });
    await auth.register(creds);
    await expect(auth.register(creds)).rejects.toMatchObject({ code: 'email_taken' });
  });

  it('пускает по верному паролю и выдаёт сессию', async () => {
    const { guardianId } = await auth.register(creds);
    const { token } = await auth.login({ email: creds.email, password: creds.password });
    const g = await auth.guardianByToken(token);
    expect(g?.id).toBe(guardianId);
  });

  it('не выдаёт, какие адреса зарегистрированы', async () => {
    await auth.register(creds);
    const wrongPassword = await auth.login({ email: creds.email, password: 'не-тот-пароль' }).catch((e: AuthError) => e);
    const wrongEmail = await auth.login({ email: 'нет@example.com', password: creds.password }).catch((e: AuthError) => e);
    expect((wrongPassword as AuthError).code).toBe('invalid_credentials');
    expect((wrongEmail as AuthError).code).toBe('invalid_credentials');
    expect((wrongPassword as AuthError).message).toBe((wrongEmail as AuthError).message);
  });

  it('токен сессии не лежит в базе в открытом виде', async () => {
    await auth.register(creds);
    const { token } = await auth.login({ email: creds.email, password: creds.password });
    const row = await prisma.session.findFirstOrThrow();
    expect(row.tokenHash).not.toBe(token);
    expect(await prisma.session.findFirst({ where: { tokenHash: token } })).toBeNull();
  });

  it('истёкшая сессия не пускает и вычищается', async () => {
    await auth.register(creds);
    const { token } = await auth.login({ email: creds.email, password: creds.password });
    await prisma.session.updateMany({ data: { expiresAt: new Date(Date.now() - 1000) } });
    expect(await auth.guardianByToken(token)).toBeNull();
    expect(await prisma.session.count()).toBe(0);
  });

  it('выход убивает сессию', async () => {
    await auth.register(creds);
    const { token } = await auth.login({ email: creds.email, password: creds.password });
    await auth.logout(token);
    expect(await auth.guardianByToken(token)).toBeNull();
  });

  it('неизвестный токен не пускает', async () => {
    expect(await auth.guardianByToken('выдуманный-токен')).toBeNull();
  });
});

describe('второй фактор', () => {
  const codeFor = (secret: string): string =>
    new OTPAuth.TOTP({ issuer: 'MyKids', secret: OTPAuth.Secret.fromBase32(secret) }).generate();

  it('включается только по верному коду', async () => {
    const { guardianId } = await auth.register(creds);
    const { secret } = auth.beginTotpSetup(creds.email);
    await expect(auth.confirmTotp(guardianId, secret, '000000'))
      .rejects.toMatchObject({ code: 'invalid_totp' });
    await auth.confirmTotp(guardianId, secret, codeFor(secret));
    const g = await prisma.guardian.findUniqueOrThrow({ where: { id: guardianId } });
    expect(g.totpSecret).toBe(secret);
  });

  it('после включения вход без кода не проходит', async () => {
    const { guardianId } = await auth.register(creds);
    const { secret } = auth.beginTotpSetup(creds.email);
    await auth.confirmTotp(guardianId, secret, codeFor(secret));

    await expect(auth.login({ email: creds.email, password: creds.password }))
      .rejects.toMatchObject({ code: 'totp_required' });
    await expect(auth.login({ email: creds.email, password: creds.password, totp: '000000' }))
      .rejects.toMatchObject({ code: 'invalid_totp' });

    const ok = await auth.login({ email: creds.email, password: creds.password, totp: codeFor(secret) });
    expect(ok.token).toBeTruthy();
  });

  it('отключается только по паролю', async () => {
    const { guardianId } = await auth.register(creds);
    const { secret } = auth.beginTotpSetup(creds.email);
    await auth.confirmTotp(guardianId, secret, codeFor(secret));
    await expect(auth.disableTotp(guardianId, 'не-тот-пароль'))
      .rejects.toMatchObject({ code: 'invalid_credentials' });
    await auth.disableTotp(guardianId, creds.password);
    const g = await prisma.guardian.findUniqueOrThrow({ where: { id: guardianId } });
    expect(g.totpSecret).toBeNull();
  });
});

describe('токены устройств', () => {
  it('выдаются один раз и хранятся хешем', async () => {
    const { childId } = await seedFamily();
    const { deviceId, token } = await auth.enrollDevice({ childId, platform: 'windows', name: 'ПК' });
    const row = await prisma.device.findUniqueOrThrow({ where: { id: deviceId } });
    expect(row.tokenHash).not.toBe(token);
    expect((await auth.deviceByToken(token))?.id).toBe(deviceId);
  });

  it('отмечают момент последней связи', async () => {
    const { childId } = await seedFamily();
    const { deviceId, token } = await auth.enrollDevice({ childId, platform: 'windows', name: 'ПК' });
    expect((await prisma.device.findUniqueOrThrow({ where: { id: deviceId } })).lastSeenAt).toBeNull();
    await auth.deviceByToken(token);
    expect((await prisma.device.findUniqueOrThrow({ where: { id: deviceId } })).lastSeenAt).not.toBeNull();
  });

  it('отозванное устройство перестаёт пускать', async () => {
    const { childId } = await seedFamily();
    const { deviceId, token } = await auth.enrollDevice({ childId, platform: 'windows', name: 'ПК' });
    await auth.revokeDevice(deviceId);
    expect(await auth.deviceByToken(token)).toBeNull();
  });

  it('неизвестный токен устройства не пускает', async () => {
    expect(await auth.deviceByToken('выдуманный')).toBeNull();
  });
});
