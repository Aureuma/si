#!/usr/bin/env node

'use strict';

const { ensureBinaryAsync, execBinary } = require('../lib/runtime');

async function main() {
  try {
    const binaryPath = await ensureBinaryAsync();
    const exitCode = execBinary(binaryPath, process.argv.slice(2));
    process.exit(exitCode);
  } catch (err) {
    const message = err && err.message ? err.message : String(err);
    process.stderr.write(`si npm launcher error: ${message}\n`);
    process.exit(1);
  }
}

void main();
