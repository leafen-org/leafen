const TRANSIENT_STATUS = 503;
const MAX_ATTEMPTS = 2;

export async function runBundleCheckLifecycle({ request, url }) {
  if (typeof request !== 'function' || typeof url !== 'string' || !url) {
    throw new TypeError('bundle check requires one request owner and one URL');
  }

  const statuses = [];
  let response;
  for (let attempt = 0; attempt < MAX_ATTEMPTS; attempt += 1) {
    response = await request(url);
    statuses.push(response.status);
    if (response.status !== TRANSIENT_STATUS) break;
  }
  return {
    attempts: statuses.length,
    response,
    statuses,
  };
}
