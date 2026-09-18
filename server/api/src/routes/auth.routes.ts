import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import type { Services } from '../app.js';
import { HttpError } from '../app.js';

const registerBody = z.object({
  familyName: z.string().min(1).max(100),
  email: z.string().email(),
  password: z.string().min(10).max(200),
});

const loginBody = z.object({
  email: z.string().email(),
  password: z.string().min(1),
  totp: z.string().regex(/^\d{6}$/).optional(),
});

export function registerAuthRoutes(app: FastifyInstance, s: Services): void {
  app.post('/auth/register', async (req, reply) => {
    const body = registerBody.parse(req.body);
    const result = await s.auth.register(body);
    const session = await s.auth.login({ email: body.email, password: body.password });
    return reply.status(201).send({ ...result, ...session });
  });

  app.post('/auth/login', async (req) => {
    const body = loginBody.parse(req.body);
    return s.auth.login(body);
  });

  app.post('/auth/logout', { preHandler: app.requireGuardian }, async (req, reply) => {
    const header = req.headers.authorization ?? '';
    await s.auth.logout(header.slice('Bearer '.length).trim());
    return reply.status(204).send();
  });

  app.get('/auth/me', { preHandler: app.requireGuardian }, async (req) => {
    const g = req.guardian!;
    return {
      id: g.id, email: g.email, role: g.role,
      familyId: g.familyId, totpEnabled: g.totpSecret !== null,
    };
  });

  app.post('/auth/totp/begin', { preHandler: app.requireGuardian }, async (req) => {
    return s.auth.beginTotpSetup(req.guardian!.email);
  });

  app.post('/auth/totp/confirm', { preHandler: app.requireGuardian }, async (req, reply) => {
    const body = z.object({
      secret: z.string().min(16),
      code: z.string().regex(/^\d{6}$/),
    }).parse(req.body);
    await s.auth.confirmTotp(req.guardian!.id, body.secret, body.code);
    return reply.status(204).send();
  });

  app.post('/auth/totp/disable', { preHandler: app.requireGuardian }, async (req, reply) => {
    const body = z.object({ password: z.string().min(1) }).parse(req.body);
    await s.auth.disableTotp(req.guardian!.id, body.password);
    return reply.status(204).send();
  });

  // Смена роли доступна только владельцу: иначе приглашённый родитель
  // мог бы повысить себя и отвязать устройства.
  app.post('/auth/guardians', { preHandler: app.requireGuardian }, async (req, reply) => {
    if (req.guardian!.role !== 'owner') {
      throw new HttpError(403, 'forbidden', 'Добавлять родителей может только владелец семьи.');
    }
    const body = z.object({
      email: z.string().email(),
      password: z.string().min(10),
      role: z.enum(['parent', 'viewer']),
    }).parse(req.body);

    const created = await s.auth.register({
      familyName: 'временная', email: body.email, password: body.password,
    });
    // register создаёт свою семью; переводим в семью владельца.
    await s.prisma.guardian.update({
      where: { id: created.guardianId },
      data: { familyId: req.guardian!.familyId, role: body.role },
    });
    await s.prisma.family.delete({ where: { id: created.familyId } });
    return reply.status(201).send({ guardianId: created.guardianId });
  });
}
