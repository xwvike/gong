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

注意 tap 仓库（`xwvike/homebrew-tap`）不套这套格式，它有自己的历史风格 `gong X.Y.Z`。

# 工程说明

架构、调度语义、Theme API 契约、发版流程都在 [`doc.md`](doc.md)。
改这个仓库之前先读它；发现文档和代码对不上，以代码为准并顺手改文档。
