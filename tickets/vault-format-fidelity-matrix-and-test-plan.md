# Ticket: Vault Dotenv Format Fidelity and Parsing Correctness

Date: 2026-02-10
Owner: Codex
Status: Completed

## Objective

Validate and harden `si vault` dotenv handling so that read/parse/write and crypto operations preserve file intent and formatting where expected, while documenting canonicalization behavior where formatting is intentionally normalized.

The scope covers:
- parser and renderer fidelity
- write atomicity and newline/mode behavior
- encryption/decryption correctness across quoting/comment variants
- duplicate-key semantics consistency across readers and scanners
- explicit expectations for `fmt` (canonicalization) versus non-`fmt` flows (preservation)

## In-Scope Operations

- `ParseDotenv`, `DotenvFile.Bytes`, `Lookup`, `Set`, `Unset`
- assignment/comment splitting and normalization logic
- `WriteDotenvFileAtomic`
- `NormalizeDotenvValue`
- `EncryptDotenvValues`, `DecryptEnv`, `ScanDotenvEncryption`, `Entries`
- `FormatVaultDotenv`

## Out of Scope

- integration tests that require a full `si vault` command runtime with trust store + key backend + subprocess orchestration
- external keyring behavior (OS-dependent)

## Behavior Contract (Preservation vs Canonicalization)

1. Non-`fmt` mutations (`set`, `unset`, `encrypt`) should preserve surrounding layout as much as possible:
   - keep section dividers/comments untouched unless directly affected by insertion/removal
   - keep existing line endings (`\n` vs `\r\n`)
   - keep inline comments attached to assignments
   - preserve leading indentation and `export` on updated keys
2. `fmt` is allowed to canonicalize style:
   - normalize header/comment spacing
   - normalize section/divider layout
   - normalize key/value spacing around `=`
3. Duplicate key resolution should be consistent with dotenv last-wins semantics for effective values.

## Test Matrix

Legend:
- `Status`: `TODO`, `IN_PROGRESS`, `PASS`, `FAIL`, `FIXED`
- `Expected Form`: the exact or semantic expected output behavior

| ID | Area | Condition / Edge Case | Action | Expected Form | Status | Notes |
|---|---|---|---|---|---|---|
| M01 | Parse/Render | LF file | parse + bytes | byte-identical roundtrip | PASS | `TestDotenvParseBytesRoundTripLF` |
| M02 | Parse/Render | CRLF file | parse + bytes | CRLF preserved | PASS | `TestDotenvParseBytesRoundTripCRLF` |
| M03 | Parse/Render | final line without newline | set append | appended line gets default newline; existing content preserved | PASS | `TestDotenvSetAppendsWhenFinalLineHasNoNewline` |
| M04 | Assignment | leading indentation + `export` | set existing key | indentation and `export` preserved | PASS | `TestDotenvSetPreservesExportIndentAndEqSpacing` |
| M05 | Assignment | inline comment with spaces | set existing key | comment spacing preserved on non-`fmt` mutation | PASS | `TestDotenvSetPreservesInlineCommentSpacing` |
| M06 | Assignment | `#` inside unquoted token (`abc#def`) | parse | not treated as comment | PASS | `TestSplitValueAndCommentUnquotedHashWithoutSpaceNotAComment` |
| M07 | Assignment | `value # comment` | parse | comment split at space-hash boundary | PASS | `TestSplitValueAndCommentUnquotedWithSpaceHashIsComment` |
| M08 | Assignment | single-quoted with hash inside | parse | hash stays in value | PASS | `TestSplitValueAndCommentSingleQuotedHashNotComment` |
| M09 | Assignment | double-quoted with escaped quote and hash | parse | quoted value parsed; comment only after closing quote | PASS | `TestSplitValueAndCommentDoubleQuotedCommentAfterQuote` |
| M10 | Value Norm | single quotes | normalize | outer quotes removed | PASS | `TestNormalizeDotenvValueSingleQuoted` |
| M11 | Value Norm | double quotes with escapes/newline | normalize | unescaped string returned | PASS | `TestNormalizeDotenvValueDoubleQuotedEscapes` |
| M12 | Value Norm | invalid quoted string | normalize | error returned | FIXED | parser hardened; `TestNormalizeDotenvValueInvalidDoubleQuoteReturnsError` |
| M13 | Set/Unset | set missing key no section | set | key appended, no extra blank lines | PASS | `TestDotenvSetAppendsWithoutExtraBlankLine` |
| M14 | Set/Unset | set missing key in missing section | set section | canonical section scaffold appended | PASS | `TestDotenvSetInMissingSectionAppendsScaffold` |
| M15 | Set/Unset | set key in existing section with trailing blanks | set section | insert before trailing blanks | PASS | `TestDotenvSetInSectionInsertsBeforeTrailingBlankLines` |
| M16 | Set/Unset | unset key with duplicates | unset | all duplicates removed | PASS | `TestDotenvUnsetRemovesAllOccurrences` |
| M17 | Lookup | duplicate keys | lookup | last value wins | PASS | `TestDotenvLookupLastWins` |
| M18 | Entries | duplicate keys | entries | effective entry reflects last value (last-wins) | FIXED | `Entries` updated; `TestEntriesDuplicateKeysUseLastValue` |
| M19 | Decrypt | mixed encrypted/plaintext keys | decrypt | encrypted decrypted; plaintext normalized | PASS | `TestDecryptEnvWithMixedValuesAndQuotes` |
| M20 | Decrypt | duplicate keys | decrypt | map reflects last value wins | PASS | `TestDecryptEnvDuplicateKeysLastValueWins` |
| M21 | Encrypt | plaintext + comments + export | encrypt | only value replaced with ciphertext, comment/export/leading preserved | PASS | `TestEncryptDotenvValuesPreservesAssignmentLayout` |
| M22 | Encrypt | already encrypted + no reencrypt | encrypt | no mutation, skipped counted | PASS | `TestEncryptDotenvValuesIdempotentWithoutReencrypt` |
| M23 | Encrypt | already encrypted + reencrypt | encrypt reencrypt | ciphertext changes, plaintext semantic unchanged | PASS | `TestEncryptDotenvValuesReencryptChangesCiphertextButNotPlaintext` |
| M24 | Encrypt | missing recipients | encrypt | error | PASS | `TestEncryptDotenvValuesErrorsWithoutRecipients` |
| M25 | Scan | empty / plaintext / encrypted mix | scan | keys classified correctly | PASS | `TestScanDotenvEncryptionClassifiesValues` |
| M26 | Scan | invalid ciphertext payload | scan | validation error | PASS | `TestScanDotenvEncryptionErrorsOnInvalidCiphertext` |
| M27 | Atomic Write | existing file mode `0600` | atomic write | mode preserved | PASS | `TestWriteDotenvFileAtomicPreservesMode` |
| M28 | Atomic Write | new file path | atomic write | file created with default mode | PASS | `TestWriteDotenvFileAtomicCreatesMissingDirectories` |
| M29 | Fmt | messy header/sections/comments | fmt | canonical output form | PASS | `TestFormatVaultDotenvCanonicalizesHeaderAndSections` |
| M30 | Fmt | already canonical | fmt | no changes | PASS | `TestFormatVaultDotenvNoChangeForCanonicalInput` |
| M31 | Header | existing header missing recipient | ensure header | recipient appended into header block only | PASS | `TestEnsureVaultHeaderAddsMissingRecipientOnly` |
| M32 | Header | no header + recipients | ensure header | header prepended with blank-line separation | PASS | `TestEnsureVaultHeaderPrependsWhenMissing` |
| M33 | Header | remove recipient | remove | target recipient removed only | PASS | `TestRemoveRecipientOnlyRemovesTarget` |
| M34 | Section Range | canonical divider before next section | set in section | insert position excludes next section divider | PASS | `TestDotenvSetInSectionPreservesNextSectionDivider` |
| M35 | Assignment | RHS comment-only (`=   # note`) | parse | empty value + preserved comment | PASS | `TestSplitValueAndCommentCommentOnlyRHS` |
| M36 | Set/Unset | set existing key with equivalent value | set | no-op (`changed=false`) and bytes unchanged | PASS | `TestDotenvSetNoOpWhenValueUnchanged` |
| M37 | Set/Unset | section update with custom `=` spacing | set section | spacing around `=` preserved | PASS | `TestDotenvSetInSectionPreservesEqSpacingOnUpdate` |
| M38 | Value Norm | lone single quote (`'`) | normalize | error returned | FIXED | `TestNormalizeDotenvValueLoneSingleQuoteReturnsError` |
| M39 | Value Norm | lone double quote (`\"`) | normalize | error returned | FIXED | `TestNormalizeDotenvValueLoneDoubleQuoteReturnsError` |
| M40 | Header | CRLF input file | ensure header | inserted header uses CRLF | PASS | `TestEnsureVaultHeaderPreservesCRLF` |
| M41 | Header | duplicate recipient args | ensure header | recipients deduped in emitted header | PASS | `TestEnsureVaultHeaderDedupesRecipientsInput` |
| M42 | Header | remove absent recipient | remove | no-op, file unchanged | PASS | `TestRemoveRecipientNoOpWhenMissing` |
| M43 | Fmt | repeated blank lines in body | fmt | collapse to canonical blank spacing | PASS | `TestFormatVaultDotenvCollapsesExtraBlankLines` |
| M44 | Fmt | unknown preamble content before section | fmt | preserve unknown preamble lines | PASS | `TestFormatVaultDotenvPreservesUnknownPreambleLines` |
| M45 | Scan | malformed quoted plaintext | scan | error surfaced | PASS | `TestScanDotenvEncryptionErrorsOnInvalidQuotedPlaintext` |
| M46 | Decrypt | malformed quoted plaintext | decrypt | error surfaced | PASS | `TestDecryptEnvErrorsOnInvalidQuotedPlaintext` |
| M47 | Encrypt | malformed quoted plaintext | encrypt | error surfaced | PASS | `TestEncryptDotenvValuesErrorsOnInvalidQuotedPlaintext` |

## Execution Plan

1. Add matrix ticket and mark progress as tests are implemented.
2. Expand unit tests across `dotenv`, `value`, `entries`, `secrets`, `fmt`, `header`.
3. Run `go test` for `tools/si/internal/vault`.
4. Fix failures/regressions.
5. Re-run tests and finalize matrix statuses.
6. Commit in logical chunks with explanatory commit messages + bodies.

## Progress Log

- 2026-02-10: Ticket created, matrix drafted. Test implementation started.
- 2026-02-10: Added/expanded unit tests across `dotenv`, `value`, `entries`, `secrets`, `scan`, `fmt`, and `header`.
- 2026-02-10: Found and fixed two correctness issues:
  - duplicate-key handling in `Entries` now returns last-wins effective values.
  - unmatched quoted values now return normalization errors instead of silently passing through.
- 2026-02-10: Preserved assignment layout during non-`fmt` mutation/encryption updates (leading indent, `export`, spacing around `=` and pre-comment spacing).
- 2026-02-10: Validation runs:
  - `go test ./tools/si/internal/vault -count=1` => PASS
  - `go test ./tools/si/... -count=1` => PASS
- 2026-02-10: Extended matrix beyond initial pass (`M35`-`M47`) and repeated full validation:
  - `go test ./tools/si/internal/vault -count=1` => PASS
  - `go test ./tools/si/... -count=1` => PASS
