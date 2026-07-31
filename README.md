# gong

I'm outta here!

## 安装

```bash
brew install xwvike/tap/gong
gong on
```

默认启用两条定时：#1 12:00、#2 18:00，周一到周五。


```bash
gong on             # 启用 gong，让它提醒你该溜了！
gong set            # 进入定时器管理
gong ls             # 列出现有定时
gong themes         # 列出可用主题
gong vis <theme>    # 预览一个主题
gong stop           # 掐掉正在播的浮层
gong off            # 关掉 gong，但保留配置和程序
```

## 主题

### default

<img src="docs/default.png" alt="default" width="720">

### tunnel

<img src="docs/tunnel.gif" alt="tunnel" width="720">

### noise

<img src="docs/noise.gif" alt="noise" width="720">

## 卸载gong

```bash
gong uninstall
```

参数 `--purge` 会同时清除自定义主题目录 `~/.config/gong`。

**避免直接 `brew uninstall gong`** ——  会导致plist 会留在
`~/Library/LaunchAgents`。
