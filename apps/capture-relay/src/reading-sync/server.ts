import Fastify, { type FastifyRequest } from 'fastify';
import { ReadingDatabase, type ReadingLimits } from './database';
import { ReadingPairings } from './pairing';
import { check, credential, digest, hash, id, objectBody, ReadingError } from './validation';
import type { ReadingPublish } from '@guizhi/shared/types/reading-sync';

export function createReadingServer(options: { database: string; bootstrapCredential: string; limits?: ReadingLimits }) {
  const bootstrapHash = hash(credential(options.bootstrapCredential));
  const db = new ReadingDatabase(options.database, options.limits), pairs = new ReadingPairings(db);
  const app = Fastify({ logger: false, trustProxy: false, bodyLimit: 12 * 1024 * 1024, requestTimeout: 30000 });
  const bearer = (request: FastifyRequest) => {
    const value = request.headers.authorization?.match(/^Bearer ([A-Za-z0-9_-]{43})$/)?.[1];
    check(value, 'unauthorized', 401); return value;
  };
  const auth = (request: FastifyRequest, writer = false) => {
    const library = id((request.params as { library: string }).library);
    db.auth(library, bearer(request), writer); return library;
  };
  app.addHook('onRequest', async (request, reply) => {
    reply.header('Cache-Control', 'no-store').header('X-Content-Type-Options', 'nosniff');
    if (request.url === '/healthz') return;
    check(!request.headers.origin && !request.headers.cookie, 'native_only', 403);
    check(request.headers['x-guizhi-reading'] === '1', 'protocol_mismatch', 426);
    db.rate(hash(request.ip));
  });
  app.setErrorHandler((error, _request, reply) => {
    const known = error instanceof ReadingError;
    const httpCode = (error as { statusCode?: number })?.statusCode;
    const status = known ? error.statusCode : httpCode === 413 ? 413 : httpCode === 400 ? 400 : 500;
    if (status === 429) reply.header('Retry-After', '60');
    reply.code(status).send({ error: known ? error.code : status === 500 ? 'reading_service_failed' : 'invalid_request' });
  });
  app.get('/healthz', async () => ({ ok: true, service: 'guizhi-reading', protocol: 1 }));
  app.post('/v1/reading/libraries', async request => {
    check(hash(bearer(request)) === bootstrapHash, 'unauthorized', 401);
    const body = objectBody(request.body, ['libraryId', 'generation', 'writerCredential']);
    db.provision(id(body.libraryId), id(body.generation), credential(body.writerCredential)); return { created: true };
  });
  app.get('/v1/reading/:library/head', async request => db.head(auth(request)));
  app.put('/v1/reading/:library/objects/:digest', async request => {
    const library = auth(request, true), body = objectBody(request.body, ['token']);
    db.put(library, digest((request.params as { digest: string }).digest), body.token as string); return { stored: true };
  });
  app.get('/v1/reading/:library/objects/:digest', async request => {
    const library = auth(request), value = digest((request.params as { digest: string }).digest);
    const row = db.get<{ token: string }>(`SELECT o.token FROM reading_objects o JOIN reading_refs r
      ON r.library=o.library AND r.digest=o.digest WHERE o.library=? AND o.digest=?`, library, value);
    check(row, 'not_found', 404); return row;
  });
  app.put('/v1/reading/:library/head', async request => {
    const library = auth(request, true);
    const body = objectBody(request.body, ['sequence', 'parentDigest', 'manifestToken', 'objectDigests']);
    return db.publish(library, body as unknown as ReadingPublish);
  });
  app.post('/v1/reading/:library/pairs/:pair', async request => pairs.create(auth(request, true), id((request.params as { pair: string }).pair)));
  app.get('/v1/reading/:library/pairs/:pair', async request => pairs.status(auth(request, true), id((request.params as { pair: string }).pair)));
  app.post('/v1/reading/:library/pairs/:pair/claim', async request => {
    const params = request.params as { library: string; pair: string }, body = objectBody(request.body, ['deviceId', 'proof']);
    return pairs.claim(id(params.library), id(params.pair), id(body.deviceId), bearer(request), digest(body.proof));
  });
  app.get('/v1/reading/:library/pairs/:pair/bundle', async request => {
    const params = request.params as { library: string; pair: string };
    return pairs.readerBundle(id(params.library), id(params.pair), bearer(request));
  });
  app.post('/v1/reading/:library/pairs/:pair/approve', async request => {
    const library = auth(request, true), body = objectBody(request.body, ['credentialHash', 'bundle']);
    return pairs.approve(library, id((request.params as { pair: string }).pair), digest(body.credentialHash), body.bundle as string);
  });
  app.delete('/v1/reading/:library/reader', async request => pairs.revoke(auth(request, true)));
  app.addHook('onClose', async () => db.close());
  return { app, db };
}
