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
