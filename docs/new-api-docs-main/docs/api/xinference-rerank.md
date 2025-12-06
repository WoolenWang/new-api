# Xinference 重排序格式（Rerank）

!!! warning "重要提示"
    在New API中，Xinference的rerank响应结构将被格式化为Jina的rerank响应结构，使用方式和Jina的rerank相同。**对于Dify等客户端用户**：在配置时请选择 **Jina AI** 作为供应商类型，而不是Xinference，并使用Xinference支持的模型名称。

## 📝 简介

Xinference的重排序API与Jina AI的重排序API完全兼容。请参考[Jina AI 重排序格式（Rerank）](jinaai-rerank.md)文档了解详细的使用方法、请求参数和响应格式。

## 💡 使用方法

使用Xinference重排序API时，只需将`model`参数设置为Xinference支持的重排序模型即可，其余参数和使用方式与Jina AI重排序API相同。

### 示例请求

```bash
curl https://你的newapi服务器地址/v1/rerank \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "jina-reranker-v2",
    "query": "什么是美国的首都？",
    "documents": [
      "内华达州的首府是卡森城。",
      "北马里亚纳群岛是太平洋上的一组岛屿，其首都是塞班岛。",
      "华盛顿特区（也称为华盛顿或特区，正式名称为哥伦比亚特区）是美国的首都。",
      "英语语法中的大写是在单词开头使用大写字母。英语用法与其他语言的大写不同。",
      "自美国成为一个国家之前，美国就存在死刑。截至2017年，在50个州中有30个州死刑合法。"
    ],
    "top_n": 3
  }'
```

有关更多详细信息，请参考[Jina AI 重排序格式（Rerank）](jinaai-rerank.md)文档。