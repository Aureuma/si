'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const https = require('node:https');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const pkg = require('../package.json');

const DEFAULT_REPO = 'Aureuma/si';
const CACHE_ROOT = process.env.SI_NPM_CACHE_DIR || path.join(os.homedir(), '.cache', 'si', 'npm');

function resolveTarget() {
  const platform = process.platform;
  const arch = process.arch;

  if (platform === 'darwin') {
    if (arch === 'x64') {
      return { goos: 'darwin', label: 'amd64' };
    }
    if (arch === 'arm64') {
      return { goos: 'darwin', label: 'arm64' };
    }
  }

  if (platform === 'linux') {
    if (arch === 'x64') {
      return { goos: 'linux', label: 'amd64' };
    }
    if (arch === 'arm64') {
      return { goos: 'linux', label: 'arm64' };
    }
    if (arch === 'arm') {
      return { goos: 'linux', label: 'armv7' };
    }
  }

  throw new Error(
    `unsupported platform/arch for SI npm package: ${platform}/${arch}. ` +
      'Supported: darwin (x64, arm64), linux (x64, arm64, arm).'
  );
}

function ensureTagVersion(version) {
  if (!version || typeof version !== 'string') {
    throw new Error('invalid package version');
  }
  return `v${version}`;
}

function computeSha256(filePath) {
  const hash = crypto.createHash('sha256');
  const data = fs.readFileSync(filePath);
  hash.update(data);
  return hash.digest('hex');
}

function httpGetBuffer(url) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, (res) => {
      const status = res.statusCode || 0;

      if (status >= 300 && status < 400 && res.headers.location) {
        res.resume();
        resolve(httpGetBuffer(res.headers.location));
        return;
      }

      if (status < 200 || status >= 300) {
        res.resume();
        reject(new Error(`HTTP ${status} while fetching ${url}`));
        return;
      }

      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve(Buffer.concat(chunks)));
    });

    request.on('error', reject);
  });
}

function parseChecksums(text) {
  const map = new Map();
  const lines = String(text)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);

  for (const line of lines) {
    const m = line.match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (!m) {
      continue;
    }
    map.set(m[2].trim(), m[1].toLowerCase());
  }

  return map;
}

function runTarExtract(archivePath, outDir) {
  const result = spawnSync('tar', ['-xzf', archivePath, '-C', outDir], {
    stdio: ['ignore', 'pipe', 'pipe']
  });

  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    const stderr = result.stderr ? String(result.stderr) : '';
    throw new Error(`failed to extract archive with tar: ${stderr}`.trim());
  }
}

function buildAssetContext() {
  const target = resolveTarget();
  const version = pkg.version;
  const tag = ensureTagVersion(version);
  const artifactName = `si_${version}_${target.goos}_${target.label}.tar.gz`;

  const localDir = process.env.SI_NPM_LOCAL_ARCHIVE_DIR;
  if (localDir) {
    return {
      version,
      tag,
      target,
      artifactName,
      checksumsRef: path.join(localDir, 'checksums.txt'),
      archiveRef: path.join(localDir, artifactName),
      local: true
    };
  }

  const repo = process.env.SI_NPM_GITHUB_REPO || DEFAULT_REPO;
  const baseUrl =
    process.env.SI_NPM_RELEASE_BASE_URL ||
    `https://github.com/${repo}/releases/download/${tag}`;

  return {
    version,
    tag,
    target,
    artifactName,
    checksumsRef: `${baseUrl}/checksums.txt`,
    archiveRef: `${baseUrl}/${artifactName}`,
    local: false
  };
}

async function fetchChecksums(ctx) {
  if (ctx.local) {
    return parseChecksums(await fsp.readFile(ctx.checksumsRef, 'utf8'));
  }
  const buf = await httpGetBuffer(ctx.checksumsRef);
  return parseChecksums(buf.toString('utf8'));
}

async function fetchArchiveToFile(ctx, outPath) {
  if (ctx.local) {
    await fsp.copyFile(ctx.archiveRef, outPath);
    return;
  }
  const buf = await httpGetBuffer(ctx.archiveRef);
  await fsp.writeFile(outPath, buf);
}

async function ensureBinaryAsync() {
  const ctx = buildAssetContext();
  const cacheDir = path.join(CACHE_ROOT, ctx.version, `${ctx.target.goos}-${ctx.target.label}`);
  const binaryPath = path.join(cacheDir, 'si');

  if (fs.existsSync(binaryPath)) {
    return binaryPath;
  }

  await fsp.mkdir(cacheDir, { recursive: true });

  const checksums = await fetchChecksums(ctx);
  const expectedSha = checksums.get(ctx.artifactName);
  if (!expectedSha) {
    throw new Error(`checksum entry missing for ${ctx.artifactName}`);
  }

  const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), 'si-npm-'));
  const archivePath = path.join(tmpDir, ctx.artifactName);

  try {
    await fetchArchiveToFile(ctx, archivePath);

    const actualSha = computeSha256(archivePath);
    if (actualSha !== expectedSha) {
      throw new Error(
        `checksum mismatch for ${ctx.artifactName}: expected ${expectedSha}, got ${actualSha}`
      );
    }

    runTarExtract(archivePath, tmpDir);

    const extractedBinary = path.join(
      tmpDir,
      `si_${ctx.version}_${ctx.target.goos}_${ctx.target.label}`,
      'si'
    );

    if (!fs.existsSync(extractedBinary)) {
      throw new Error(`extracted archive missing binary at ${extractedBinary}`);
    }

    const targetTmp = `${binaryPath}.tmp-${process.pid}`;
    await fsp.copyFile(extractedBinary, targetTmp);
    await fsp.chmod(targetTmp, 0o755);
    await fsp.rename(targetTmp, binaryPath);

    return binaryPath;
  } finally {
    await fsp.rm(tmpDir, { recursive: true, force: true });
  }
}

function execBinary(binaryPath, args) {
  const result = spawnSync(binaryPath, args, {
    stdio: 'inherit',
    env: process.env
  });

  if (result.error) {
    throw result.error;
  }

  if (typeof result.status === 'number') {
    return result.status;
  }

  return 1;
}

module.exports = {
  ensureBinaryAsync,
  execBinary,
  resolveTarget,
  parseChecksums
};
