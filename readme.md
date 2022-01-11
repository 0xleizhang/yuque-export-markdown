
导出步骤

1. 获取TOC目录，构建成一个树结构
2. 遍历树，构造下载任务（保存路径，下载路径）
   1. 有child节点的保存成目录，内容保存到目录下同名.md
3. 下载任务调用文档详情接口
   1. 通过html转markdown保存.md
   2. 下载文中图片替换成本地路径




# 图片解析下载及替换

```
 
GO CDK解决的问题:

![image.png](https://cdn.nlark.com/yuque/0/2021/png/290656/1639644491560-7fc174f6-dc73-4de1-9f66-4530f13dc1bb.png#clientId=u26616665-88ca-4&from=paste&height=258&id=u9e075884&margin=%5Bobject%20Object%5D&name=image.png&originHeight=515&originWidth=1058&originalType=binary&ratio=1&size=160339&status=done&style=none&taskId=u12d16e40-3ab3-4f51-99be-eae6fb55fee&width=529)

｜

![image.png](https://cdn.nlark.com/yuque/0/2021/png/290656/1639644772671-7debb2e7-028f-46f6-a9b8-9987d59efcd3.png#clientId=u26616665-88ca-4&from=paste&height=270&id=u976622e7&margin=%5Bobject%20Object%5D&name=image.png&originHeight=539&originWidth=1045&originalType=binary&ratio=1&size=190770&status=done&style=none&taskId=u0c45d547-18a5-4c38-9096-45d7d1a5a49&width=522.5)

​

```