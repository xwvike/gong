# git guidelines

- **Language**: All commit messages must be in **Simplified Chinese**.
- **Format Structure**: Use `<type>(<scope>): <subject>` strictly.
  - type and scope also must be in english, but subject must be in Chinese.
  - **Header**: Concise summary. No period at the end.
  - **Body**: Separate from header with one empty line. Focus on explaining **why** the change is needed (motivation).
- **Allowed Types**:
  - `feat`: New feature
  - `fix`: Bug fix
  - `docs`: Documentation
  - `style`: Formatting (whitespace, semi-colons, etc)
  - `refactor`: Code change that neither fixes a bug nor adds a feature
  - `perf`: Performance improvements
  - `test`: Adding or correcting tests
  - `chore`: Build process or auxiliary tool changes

# 发版流程

版本号只有一个来源：**git tag**。CI 用 `${GITHUB_REF_NAME#v}` 通过 ldflags 注入
`internal/paths.Version`，所以发版**不需要改任何 Go 源码**。需要手写版本号的地方只有
`packaging/gong.rb`，它是 Homebrew formula 的唯一真相。

## 顺序

1. 功能提交推上 `main`，确认 `git status` 干净。
2. `git tag vX.Y.Z && git push origin vX.Y.Z`。标签触发 `.github/workflows/release.yml`。
3. 等 CI 跑完（`gh run watch`）。它会跑 `go test ./...`、`go vet`、`ruby -c packaging/gong.rb`、
   双架构 swiftc + lipo、打包、以及 `gong-overlay --force` 冒烟测试。**CI 没绿就不要往下走。**
4. 下载产物并**独立重算**校验值，不要直接抄 release 里的 `sha256.txt`：

   ```sh
   gh release download vX.Y.Z -R xwvike/gong -p '*.tar.gz' -p 'sha256.txt'
   shasum -a 256 gong-X.Y.Z-macos-universal.tar.gz   # 与 sha256.txt 逐字比对
   ```

5. 把 `version` 和 `sha256` 写进 `packaging/gong.rb`，提交
   `chore(release): 准备 X.Y.Z`，推送。
6. 同步 Homebrew tap（见下）。
7. `brew upgrade gong`，然后验 `gong version`、`gong themes`，再拿 `gong vis <theme>`
   冒烟至少一个主题（退出码 0、stderr 为空）。

## 同步 tap —— 这里踩过坑

tap 的工作副本在 `/opt/homebrew/Library/Taps/xwvike/homebrew-tap`。

**这个副本会静默落后。** 它不只被本项目写入，`Casks/local-mirror.rb` 由另一个项目的
goreleaser 推送，所以远端经常先于本地前进。0.1.17 那次就是没 fetch 直接用了陈旧副本，
提交长在了三个版本之前的基线上，push 被拒。

所以复制 formula **之前**必须先对齐远端，且不要从这个本地副本另行 clone：

```sh
cd /opt/homebrew/Library/Taps/xwvike/homebrew-tap
git fetch origin main
git merge --ff-only origin/main          # 不能 ff 就先停下来查清楚
cp ~/Project/gong/packaging/gong.rb Formula/gong.rb
ruby -c Formula/gong.rb
git diff                                 # 预期只有 version + sha256 两行
git commit -am "gong X.Y.Z" && git push origin main
```

tap 的提交信息用 `gong X.Y.Z`，**不套用上面那套 conventional commit 格式** —— 那是本仓库的
约定，tap 有自己的历史风格。

## 其它

- formula 的 `test do` 块里写死了主题名（当前是 `led`）。改名或删除主题时，必须在同一个
  提交里改掉它，否则 CI 的 formula 校验会在下一次发版才炸。
- 提交被 SSH 签名（1Password agent）。agent 没解锁时 `git commit` 会报
  `failed to fill whole buffer` 或直接挂住等 UI 授权。这时**不要重试到超时**，
  报告状态让人去解锁再继续。
- 中途产物（tarball、编译缓存、临时 clone）一律放会话专属的临时目录，不要往 `/private/tmp`
  裸撒 —— 那里的 Go/Swift 编译缓存很快能堆到 GB 级。
