---
hide:
  - footer
---

<style>
  .md-typeset .grid.cards > ul {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr));
    gap: 1rem;
    margin: 1em 0;
  }
  
  .md-typeset .grid.cards > ul > li {
    border: none;
    border-radius: 0.8rem;
    box-shadow: var(--md-shadow-z2);
    padding: 1.5rem;
    transition: transform 0.25s, box-shadow 0.25s;
    background: linear-gradient(135deg, var(--md-primary-fg-color), var(--md-accent-fg-color));
    color: var(--md-primary-bg-color);
  }

  .md-typeset .grid.cards > ul > li:hover {
    transform: scale(1.02);
    box-shadow: var(--md-shadow-z3);
  }

  .md-typeset .grid.cards > ul > li > hr {
    margin: 0.8rem 0;
    border: none;
    border-bottom: 2px solid var(--md-primary-bg-color);
    opacity: 0.2;
  }

  .md-typeset .grid.cards > ul > li > p {
    margin: 0.5rem 0;
  }

  .md-typeset .grid.cards > ul > li > p > em {
    color: var(--md-primary-bg-color);
    opacity: 0.8;
    font-style: normal;
  }

  .md-typeset .grid.cards > ul > li > p > .twemoji {
    font-size: 2.5rem;
    display: block;
    margin: 0.5rem auto;
  }

  .md-typeset .grid.cards > ul > li a {
    display: inline-flex;
    align-items: center;
    margin-top: 1.2em;
    padding: 0.5em 1.2em;
    color: white;
    background-color: rgba(255, 255, 255, 0.15);
    border-radius: 2em;
    transition: all 0.3s ease;
    font-weight: 500;
    font-size: 0.9em;
    letter-spacing: 0.03em;
    box-shadow: 0 3px 6px rgba(0, 0, 0, 0.1);
    position: relative;
    overflow: hidden;
    text-decoration: none;
  }

  .md-typeset .grid.cards > ul > li a:hover {
    background-color: rgba(255, 255, 255, 0.25);
    text-decoration: none;
    box-shadow: 0 5px 12px rgba(0, 0, 0, 0.2);
    transform: translateX(5px);
  }

  .md-typeset .grid.cards > ul > li a:after {
    content: "→";
    opacity: 0;
    margin-left: -15px;
    transition: all 0.2s ease;
  }

  .md-typeset .grid.cards > ul > li a:hover:after {
    opacity: 1;
    margin-left: 5px;
  }
</style>

# 帮助支持

## 💫 支持服务

<div class="grid cards" markdown>

-   :material-chat-question:{ .twemoji }

    **常见问题**

    ---

    查看常见问题解答，快速解决您的疑惑：
    
    [问题解答 →](faq.md)

-   :material-account-group:{ .twemoji }

    **社区交流**

    ---

    加入我们的社区，与其他用户交流：
    
    [QQ交流群 →](community-interaction.md)

-   :material-bug:{ .twemoji }

    **问题反馈**

    ---

    遇到问题？向我们反馈：
    
    [提交问题 →](feedback-issues.md)

-   :material-coffee:{ .twemoji }

    **支持我们**

    ---

    如果您觉得项目对您有帮助：
    
    [请我们喝咖啡 →](buy-us-a-coffee.md)

</div>

## 📖 支持说明

!!! tip "获取帮助"
    我们提供多种方式帮助您解决问题：

    1. **查看文档**：大多数问题都可以在文档中找到答案
    2. **常见问题**：浏览常见问题解答，快速找到解决方案
    3. **社区交流**：加入QQ群，与其他用户交流经验
    4. **问题反馈**：在GitHub上提交issue，我们会及时处理

!!! info "关于赞助"
    New API 是一个完全免费的开源项目，我们不强制要求任何形式的赞助。
    但如果您觉得项目对您有帮助，欢迎给我们买杯咖啡，这将帮助我们：

    - 维护和升级服务器
    - 开发新功能
    - 提供更好的文档
    - 建设更好的社区

!!! warning "注意事项"
    在寻求帮助时，请注意：

    - 提问前请先查看文档和常见问题
    - 提供足够的信息以便我们理解和复现问题
    - 遵守社区规则，保持友善的交流氛围
    - 耐心等待回复，我们会尽快处理您的问题 