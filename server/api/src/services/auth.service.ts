import { hash, verify } from '@node-rs/argon2';
import { randomBytes, createHash, timingSafeEqual } from 'node:crypto';
import * as OTPAuth from 'otpauth';
import type { PrismaClient, Guardian, Device } from '@prisma/client';

export class AuthError extends Error {
  constructor(readonly code: string, message: string) {
    super(message);
  }
}

const SESSION_TTL_MS = 7 * 24 * 3600 * 1000;

/** Токены хранятся только хешем: утечка базы не должна давать вход. */
function hashToken(token: string): string {
  return createHash('sha256').update(token).digest('hex');
}

function newToken(): string {
  return randomBytes(32).toString('base64url');
}

/** Сравнение секретов постоянным временем. */
function safeEqual(a: string, b: string): boolean {
  const ba = Buffer.from(a);
  const bb = Buffer.from(b);
  return ba.length === bb.length && timingSafeEqual(ba, bb);
}

export class AuthService {
  constructor(private readonly prisma: PrismaClient) {}

  async register(input: {
    familyName: string; email: string; password: string;
  }): Promise<{ guardianId: string; familyId: string }> {
    if (input.password.length < 10) {
      throw new AuthError('weak_password', 'Пароль должен быть не короче 10 символов.');
    }
    const existing = await this.prisma.guardian.findUnique({ where: { email: input.email } });
    if (existing) throw new AuthError('email_taken', 'Такой адрес уже зарегистрирован.');

    const family = await this.prisma.family.create({ data: { name: input.familyName } });
    const guardian = await this.prisma.guardian.create({
      data: {
        familyId: family.id,
        email: input.email,
        passwordHash: await hash(input.password),
        role: 'owner',
      },
    });
    return { guardianId: guardian.id, familyId: family.id };
  }

  /**
   * Вход. При включённом TOTP код обязателен.
   *
   * Неверный адрес и неверный пароль дают одну и ту же ошибку: иначе форма
   * входа превращается в способ узнать, какие адреса зарегистрированы.
   */
  async login(input: { email: string; password: string; totp?: string | undefined }): Promise<{ token: string; expiresAt: Date }> {
    const guardian = await this.prisma.guardian.findUnique({ where: { email: input.email } });
    const invalid = new AuthError('invalid_credentials', 'Неверный адрес или пароль.');
    if (!guardian) {
      // Считаем хеш всё равно, чтобы по времени ответа нельзя было отличить
      // несуществующий адрес от неверного пароля.
      await hash(input.password);
      throw invalid;
    }
    if (!(await verify(guardian.passwordHash, input.password))) throw invalid;

    if (guardian.totpSecret !== null) {
      if (!input.totp) throw new AuthError('totp_required', 'Нужен код из приложения-аутентификатора.');
      if (!this.checkTotp(guardian.totpSecret, input.totp)) {
        throw new AuthError('invalid_totp', 'Неверный код подтверждения.');
      }
    }
    return this.issueSession(guardian.id);
  }

  private async issueSession(guardianId: string): Promise<{ token: string; expiresAt: Date }> {
    const token = newToken();
    const expiresAt = new Date(Date.now() + SESSION_TTL_MS);
    await this.prisma.session.create({
      data: { guardianId, tokenHash: hashToken(token), expiresAt },
    });
    return { token, expiresAt };
  }

  /** Родитель по токену сессии; null, если токен неизвестен или истёк. */
  async guardianByToken(token: string): Promise<Guardian | null> {
    const session = await this.prisma.session.findUnique({
      where: { tokenHash: hashToken(token) },
      include: { guardian: true },
    });
    if (!session) return null;
    if (session.expiresAt.getTime() <= Date.now()) {
      await this.prisma.session.delete({ where: { id: session.id } }).catch(() => undefined);
      return null;
    }
    return session.guardian;
  }

  async logout(token: string): Promise<void> {
    await this.prisma.session.deleteMany({ where: { tokenHash: hashToken(token) } });
  }

  // ------------------------------------------------------------------ TOTP

  /** Готовит секрет и ссылку для приложения-аутентификатора. */
  beginTotpSetup(email: string): { secret: string; uri: string } {
    const secret = new OTPAuth.Secret({ size: 20 });
    const totp = new OTPAuth.TOTP({ issuer: 'MyKids', label: email, secret });
    return { secret: secret.base32, uri: totp.toString() };
  }

  /** Включает второй фактор, только если код сходится. */
  async confirmTotp(guardianId: string, secret: string, code: string): Promise<void> {
    if (!this.checkTotp(secret, code)) {
      throw new AuthError('invalid_totp', 'Код не подошёл — проверьте время на телефоне.');
    }
    await this.prisma.guardian.update({ where: { id: guardianId }, data: { totpSecret: secret } });
  }

  async disableTotp(guardianId: string, password: string): Promise<void> {
    const guardian = await this.prisma.guardian.findUniqueOrThrow({ where: { id: guardianId } });
    if (!(await verify(guardian.passwordHash, password))) {
      throw new AuthError('invalid_credentials', 'Неверный пароль.');
    }
    await this.prisma.guardian.update({ where: { id: guardianId }, data: { totpSecret: null } });
  }

  private checkTotp(secret: string, code: string): boolean {
    const totp = new OTPAuth.TOTP({ issuer: 'MyKids', secret: OTPAuth.Secret.fromBase32(secret) });
    // Окно в один шаг: телефон и сервер могут разойтись на несколько секунд.
    return totp.validate({ token: code, window: 1 }) !== null;
  }

  // -------------------------------------------------------- токены устройств

  /**
   * Привязывает устройство и возвращает токен. Токен показывается один раз:
   * в базе лежит только его хеш.
   */
  async enrollDevice(input: {
    childId: string; platform: Device['platform']; name: string;
  }): Promise<{ deviceId: string; token: string }> {
    const token = newToken();
    const device = await this.prisma.device.create({
      data: {
        childId: input.childId, platform: input.platform,
        name: input.name, tokenHash: hashToken(token),
      },
    });
    return { deviceId: device.id, token };
  }

  /** Устройство по токену; null для неизвестного или отозванного. */
  async deviceByToken(token: string): Promise<Device | null> {
    const device = await this.prisma.device.findUnique({ where: { tokenHash: hashToken(token) } });
    if (!device || device.revokedAt !== null) return null;
    await this.prisma.device.update({
      where: { id: device.id }, data: { lastSeenAt: new Date() },
    });
    return device;
  }

  async revokeDevice(deviceId: string): Promise<void> {
    await this.prisma.device.update({ where: { id: deviceId }, data: { revokedAt: new Date() } });
  }

  /** Экспортируется для тестов сравнения секретов. */
  static secretsMatch = safeEqual;
}
