# Release Command

Create a new mcper release with all necessary steps.

## IMPORTANT: Workspace Verification

**BEFORE doing anything, verify you are in the correct workspace:**

```bash
pwd  # Should be /workspaces/mcper-public
git remote -v  # Should show joshcarp/mcper.git (NOT mcper-cloud!)
```

**This is the PUBLIC mcper repo (joshcarp/mcper). Do NOT confuse with mcper-cloud!**

## Usage
```
/release [version]
```

If version is not provided, auto-increment the patch version.

## Steps to Execute

1. **Verify workspace** (CRITICAL):
   ```bash
   pwd && git remote -v
   ```
   - Must be `/workspaces/mcper-public`
   - Remote must be `joshcarp/mcper.git`

2. **Determine version**:
   - If version provided, use it
   - Otherwise, read current version from `pkg/mcper/version.go` and increment patch

3. **Update version files**:
   - Update `pkg/mcper/version.go` with new version
   - Update `scripts/release/latest.json` with new version and today's date

4. **Build WASM plugins locally** to verify they compile:
   ```bash
   make wasm-build
   ```

5. **Run tests**:
   ```bash
   go test ./...
   ```

6. **Commit changes**:
   ```bash
   git add pkg/mcper/version.go scripts/release/latest.json
   git commit -m "Release v{VERSION}"
   ```

7. **Push and tag**:
   ```bash
   git push origin main
   git tag v{VERSION}
   git push origin v{VERSION}
   ```

8. **Upload to GCS** (using service account credentials):
   ```bash
   # Upload latest.json
   gsutil -o "Credentials:gs_service_key_file=/workspaces/mcper/Gemini API Client.json" \
     cp scripts/release/latest.json gs://mcper-releases/latest.json

   # Build and upload WASM plugins
   make wasm-build
   gsutil -o "Credentials:gs_service_key_file=/workspaces/mcper/Gemini API Client.json" \
     -m cp wasm/*.wasm gs://mcper-releases/latest/
   ```

9. **Watch the GitHub Actions release workflow**:
   ```bash
   gh run watch --exit-status
   ```

10. **Report the release URL**: `https://github.com/joshcarp/mcper/releases/tag/v{VERSION}`

## Error Handling

- If any step fails, stop and report the error
- If GCS credentials are not available, skip GCS upload but warn the user
- If GitHub Actions fails, provide the failure log
- **If workspace verification fails, STOP IMMEDIATELY and alert the user**
