import {
  createDiagnosticReference,
  DIAGNOSTIC_TRACE_SCHEMA,
  isDiagnosticReference,
} from './diagnosticTrace';

const PRODUCT_LOG_OPERATIONS = [
  'api.request',
  'auth.request',
  'collaboration.recovery',
  'error-boundary.chunk-recovery',
  'error-boundary.render',
  'dev-bundle-freshness.retry-exhausted',
  'file-drop.upload',
  'imported-people.request',
  'local-lock.legacy-conflict',
  'local-lock.migration-defer',
  'local-lock.settings',
  'notion-admin-api.request',
  'outbox.persistence',
  'page-analytics.request',
  'public-form.request',
  'record-cache.unavailable',
  'service-worker.install',
  'service-worker.register',
  'settings.unlocalized-backend-error',
  'sync-status.request',
  'toast.action',
  'workspace-import.request',
  'workspace-export.request',
] as const;

export type ProductLogOperation = typeof PRODUCT_LOG_OPERATIONS[number];
export type ProductErrorLogLevel = 'error' | 'warn';

export interface SafeProductErrorDetails {
  errorClass: string;
  sourcePosition: string;
  emittedAsset?: string;
  emittedLine?: number;
  emittedColumn?: number;
}

export interface SafeProductErrorMetadata {
  diagnosticReference?: unknown;
  status?: unknown;
  errorCode?: unknown;
  causeCode?: unknown;
  causeStage?: unknown;
}

export function captureErrorFactoryCaller<T extends Error>(
  error: T,
  factory: unknown,
): T {
  try {
    const captureStackTrace = Reflect.get(Error, 'captureStackTrace');
    if (typeof captureStackTrace === 'function') {
      Reflect.apply(captureStackTrace, Error, [error, factory]);
    }
  } catch {
    // Non-V8 runtimes retain the ordinary construction stack. Diagnostics
    // must never turn error creation into a second failure.
  }
  return error;
}

const PRODUCT_POSITION =
  /(?:^|[/\\])((?:cmd|internal|shared|web[/\\]src)[/\\][A-Za-z0-9_@./\\[\]-]+\.[cm]?[jt]sx?):([1-9][0-9]*)(?::[0-9]+)?(?=$|[)\s])/;
const SAFE_ERROR_CODE = /^[A-Za-z][A-Za-z0-9_.:-]{0,63}$/;
const EMITTED_POSITION = /(?:https?:\/\/[^/\s)]+)?\/(assets\/[A-Za-z0-9_.-]+\.js):([1-9][0-9]*):([1-9][0-9]*)(?=$|[)\s])/;

function safeErrorClass(error: unknown): string {
  if (error === null) return 'null';
  try {
    if (error instanceof TypeError) return 'type_error';
    if (error instanceof ReferenceError) return 'reference_error';
    if (error instanceof RangeError) return 'range_error';
    if (error instanceof SyntaxError) return 'syntax_error';
    // WebKit refuses a body that isn't JSON with a DOMException of that name.
    if (typeof DOMException !== 'undefined' && error instanceof DOMException && error.name === 'SyntaxError') {
      return 'syntax_error';
    }
    if (error instanceof URIError) return 'uri_error';
    if (error instanceof EvalError) return 'eval_error';
    if (typeof AggregateError !== 'undefined' && error instanceof AggregateError) {
      return 'aggregate_error';
    }
    if (error instanceof Error) return 'error';
  } catch {
    // Proxies can throw from prototype inspection. The primitive/object class
    // below is still total and never reads a caller-controlled property.
  }
  return `thrown_${typeof error}`;
}

function safeStack(error: unknown): string | undefined {
  if ((typeof error !== 'object' || error === null) && typeof error !== 'function') {
    return undefined;
  }
  try {
    const stack = Reflect.get(error, 'stack');
    return typeof stack === 'string' ? stack : undefined;
  } catch {
    return undefined;
  }
}

function safeErrorStatus(error: unknown): unknown {
  if (
    (typeof error !== 'object' || error === null)
    && typeof error !== 'function'
  ) {
    return undefined;
  }
  try {
    const status = Reflect.get(error, 'status');
    return status === undefined ? Reflect.get(error, 'code') : status;
  } catch {
    return undefined;
  }
}

function boundedHttpStatus(value: unknown): number | undefined {
  try {
    const status = Number(value);
    return Number.isInteger(status) && status >= 400 && status <= 599
      ? status
      : undefined;
  } catch {
    return undefined;
  }
}

function productPosition(stack: unknown, skipLoggerFrame = false): string | undefined {
  if (typeof stack !== 'string') return undefined;
  for (const line of stack.split(/\r?\n/)) {
    const match = PRODUCT_POSITION.exec(line);
    if (!match) continue;
    const path = match[1].replaceAll('\\', '/');
    if (
      path.split('/').some((segment) => segment === '.' || segment === '..')
      || (skipLoggerFrame && (
        path === 'shared/safe-error-log.ts'
        || path === 'web/src/lib/safeErrorLog.ts'
      ))
    ) {
      continue;
    }
    return `${path}:${match[2]}`;
  }
  return undefined;
}

function emittedPosition(stack: unknown) {
  if (typeof stack !== 'string') return undefined;
  for (const line of stack.split(/\r?\n/)) {
    const match = EMITTED_POSITION.exec(line);
    if (!match) continue;
    return {
      emittedAsset: match[1],
      emittedLine: Number(match[2]),
      emittedColumn: Number(match[3]),
    };
  }
  return undefined;
}

function safeProductErrorDetails(
  error: unknown,
  fallbackStack?: unknown,
): SafeProductErrorDetails {
  try {
    const errorStack = safeStack(error);
    const sourcePosition = productPosition(errorStack)
      ?? productPosition(fallbackStack, true);
    const emitted = emittedPosition(errorStack) ?? emittedPosition(fallbackStack);
    return {
      errorClass: safeErrorClass(error),
      sourcePosition: sourcePosition ?? (emitted ? 'unresolved' : 'unknown'),
      ...emitted,
    };
  } catch {
    return {
      errorClass: error === null ? 'null' : `thrown_${typeof error}`,
      sourcePosition: 'unknown',
    };
  }
}

export function logProductError(
  level: ProductErrorLogLevel,
  operation: ProductLogOperation,
  error: unknown,
  metadata: SafeProductErrorMetadata = {},
): string {
  let fallbackStack: string | undefined;
  try {
    fallbackStack = new Error().stack;
  } catch {
    fallbackStack = undefined;
  }
  const details = safeProductErrorDetails(error, fallbackStack);
  let errorDiagnosticReference: unknown;
  try {
    errorDiagnosticReference = (typeof error === 'object' && error !== null)
      || typeof error === 'function'
      ? Reflect.get(error, 'reference')
      : undefined;
  } catch {
    errorDiagnosticReference = undefined;
  }
  const diagnosticReference = isDiagnosticReference(metadata.diagnosticReference)
    ? metadata.diagnosticReference
    : isDiagnosticReference(errorDiagnosticReference)
      ? errorDiagnosticReference
      : createDiagnosticReference();
  const status = boundedHttpStatus(
    metadata.status === undefined ? safeErrorStatus(error) : metadata.status,
  );
  const errorCode = typeof metadata.errorCode === 'string'
    && SAFE_ERROR_CODE.test(metadata.errorCode)
    ? metadata.errorCode
    : undefined;
  const causeCode = typeof metadata.causeCode === 'string'
    && SAFE_ERROR_CODE.test(metadata.causeCode)
    ? metadata.causeCode
    : undefined;
  const causeStage = typeof metadata.causeStage === 'string'
    && SAFE_ERROR_CODE.test(metadata.causeStage)
    ? metadata.causeStage
    : undefined;
  const payload = {
    diagnosticSchema: DIAGNOSTIC_TRACE_SCHEMA,
    diagnosticReference,
    operation,
    ...details,
    ...(status === undefined ? {} : { status }),
    ...(errorCode ? { errorCode } : {}),
    ...(causeCode ? { causeCode } : {}),
    ...(causeStage ? { causeStage } : {}),
  };
  const sink = level === 'error' ? console.error : console.warn;
  try {
    sink('[product-error]', payload);
  } catch {
    // A diagnostic sink is observational and must not replace product behavior.
  }
  return diagnosticReference;
}
