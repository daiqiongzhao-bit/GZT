<template>
  <div class="workspace">
    <div class="ws-head">
      <h2 class="page-title">知识库</h2>
      <div class="ws-tool">
        <div class="tabs">
          <button class="tab" :class="{ active: tab === 'knowledge' }" @click="switchTab('knowledge')">
            迷你知识库<template v-if="tab==='knowledge'&&auth.user"> · 沉淀方法/流程</template>
          </button>
          <button class="tab" :class="{ active: tab === 'logs' }" @click="switchTab('logs')">工作日志</button>
          <button class="tab" :class="{ active: tab === 'handover' }" @click="switchTab('handover')">
            交接接力<template v-if="inboxUnread>0&&tab!=='handover'">·<i class="handover-badge">{{ inboxUnread }}</i></template>
          </button>
        </div>
        <button v-if="auth.isSuper" class="btn ghost export-bundle" @click="exportBundle" title="把当前可见的知识/日志/交接及附件打包成一个 zip，便于备份或迁移到正式环境（仅超级管理员）">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" style="width:15px;height:15px;vertical-align:-2px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M7 10l5 5 5-5"/><path d="M12 15V3"/></svg>
          打包下载 zip
        </button>
      </div>
    </div>

    <!-- ========== 迷你知识库 ========== -->
    <template v-if="tab === 'knowledge'">
      <section class="panel">
        <div class="panel-head">
          <h3 class="section-title">知识条目 <span class="section-sub">{{ knowledge.length }} 条<template v-if="kbView==='trash'"> · 回收站 {{ trash.length }} 条</template></span></h3>
          <div class="head-actions kb-actions">
            <!-- ① 视图切换（互斥）：条目 / 统计 / 回收站 -->
            <div class="seg-group" role="group" aria-label="视图切换">
              <button class="seg" :class="{ on: kbView === 'list' }" @click="kbGo('list')">条目</button>
              <button class="seg" :class="{ on: kbView === 'stats' }" @click="kbGo('stats')">📊 统计</button>
              <button class="seg" :class="{ on: kbView === 'trash' }" @click="kbGo('trash')">🗑 回收站</button>
            </div>

            <span class="kb-div" aria-hidden="true"></span>

            <!-- ② 创建：模板 / 导入 -->
            <button class="btn ghost sm" @click="openTemplates" title="从模板快速新建条目">📋 模板</button>
            <button class="btn ghost sm" @click="openImport" title="粘贴 Markdown 批量导入">⬇ 导入</button>

            <!-- ③ 导出：三种格式收进一个下拉，避免平铺占位 -->
            <div v-if="kbView === 'list'" class="kb-menu-wrap" @click.stop>
              <button class="btn ghost sm" :aria-expanded="exportMenu ? 'true' : 'false'" @click="exportMenu = !exportMenu">
                ⬆ 导出 <i class="caret">▾</i>
              </button>
              <transition name="pop">
                <div v-if="exportMenu" class="kb-menu" @click.stop>
                  <button class="menu-item" @click="pickExport(exportMarkdown)"><span class="mi-ico">📝</span><span>Markdown（.md）</span></button>
                  <button class="menu-item" @click="pickExport(exportDoc)"><span class="mi-ico">📄</span><span>Word（.doc）</span></button>
                  <button class="menu-item" @click="pickExport(exportK)"><span class="mi-ico">📃</span><span>纯文本（.txt）</span></button>
                </div>
              </transition>
            </div>

            <!-- ④ 危险操作 / 主操作 -->
            <button v-if="kbView === 'trash'" class="btn ghost sm danger" @click="emptyTrash">清空回收站</button>
            <button v-if="kbView === 'list'" class="btn primary" @click="openNewK">+ 新建</button>
            <button v-else class="btn ghost" @click="kbGo('list')">← 返回列表</button>
          </div>
        </div>

        <!-- 统计看板 -->
        <div v-if="kbView==='stats'" class="kb-stats">
          <div v-if="statsLoading" class="dim" style="padding:20px 0">加载统计中…</div>
          <template v-else-if="stats">
            <div class="stat-grid">
              <div class="stat-card"><div class="stat-num">{{ stats.total }}</div><div class="stat-lbl">知识总数</div></div>
              <div class="stat-card"><div class="stat-num">{{ stats.recent_30d }}</div><div class="stat-lbl">近 30 天新增</div></div>
              <div class="stat-card"><div class="stat-num">{{ stats.my_starred }}</div><div class="stat-lbl">我的收藏</div></div>
            </div>
            <div class="stat-block">
              <div class="stat-h">分类分布</div>
              <div class="bar-chart">
                <div v-for="c in stats.categories" :key="c.name" class="bar-row">
                  <span class="bar-name">{{ c.name }}</span>
                  <span class="bar-track"><span class="bar-fill" :style="{width: barPct(c.count, stats.categories)}"></span></span>
                  <span class="bar-cnt">{{ c.count }}</span>
                </div>
                <div v-if="!stats.categories.length" class="dim">暂无分类数据</div>
              </div>
            </div>
            <div class="stat-block">
              <div class="stat-h">热门标签</div>
              <div class="tag-cloud">
                <span v-for="t in stats.tags" :key="t.name" class="tag-pill" :style="{fontSize: tagSize(t.count, stats.tags)+'px'}" @click="kTag=t.name;kbView='list';loadK()">#{{ t.name }} <i>{{ t.count }}</i></span>
                <span v-if="!stats.tags.length" class="dim">暂无标签</span>
              </div>
            </div>
            <div class="stat-block">
              <div class="stat-h">贡献者</div>
              <div class="author-list">
                <span v-for="a in stats.authors" :key="a.name" class="author-pill">{{ a.name }} <i>{{ a.count }}</i></span>
                <span v-if="!stats.authors.length" class="dim">暂无</span>
              </div>
            </div>
          </template>
        </div>

        <!-- 回收站 -->
        <div v-else-if="kbView==='trash'" class="kb-trash">
          <div v-if="!trash.length" class="empty">回收站是空的</div>
          <div v-else class="trash-list">
            <div v-for="t in trash" :key="t.id" class="trash-item">
              <div class="trash-main">
                <span class="trash-title">{{ t.title }}</span>
                <span class="dim">{{ t.category || '未分类' }} · 删除于 {{ fmtTime(t.deleted_at) }}</span>
              </div>
              <div class="trash-ops">
                <button class="btn sm ok" @click="restoreK(t.id)">恢复</button>
                <button class="btn sm danger" @click="purgeK(t.id)">彻底删除</button>
              </div>
            </div>
          </div>
        </div>

        <!-- 列表视图 -->
        <template v-else>
          <!-- 内联新增/编辑表单 -->
          <form v-if="editingK" class="edit-form" @submit.prevent="saveK">
            <div class="fg2">
              <div>
                <label class="fld">标题 *</label>
                <input v-model="kForm.title" class="glass-input" required placeholder="如：周年庆小红书发布 SOP" />
              </div>
              <div>
                <label class="fld">分类</label>
                <input v-model="kForm.category" class="glass-input" list="kCats" placeholder="如：活动SOP / 检查流程 / 话术" />
                <datalist id="kCats">
                  <option v-for="c in categories" :key="c" :value="c" />
                </datalist>
              </div>
            </div>
            <div class="fg2">
              <div>
                <label class="fld">所属父级（目录树）</label>
                <select v-model.number="kForm.parent_id" class="glass-input">
                  <option :value="0">（顶级，无父级）</option>
                  <option v-for="p in parentCandidates" :key="p.id" :value="p.id">{{ p.title }}</option>
                </select>
              </div>
              <div>
                <label class="fld">状态</label>
                <select v-model="kForm.status" class="glass-input">
                  <option value="published">已发布</option>
                  <option value="draft">草稿（仅自己可见）</option>
                </select>
              </div>
            </div>
            <div>
              <label class="fld">标签</label>
              <div class="tag-edit">
                <span v-for="(t,i) in kForm.tags" :key="t" class="chip pick-chip">{{ t }}<button type="button" class="x" @click="kForm.tags.splice(i,1)" aria-label="移除">×</button></span>
                <input v-model="kTagDraft" @keydown.enter.prevent="addKTag" class="glass-input tag-input" placeholder="输入标签后回车添加" />
              </div>
            </div>
            <div>
              <label class="fld">内容</label>
              <RichTextEditor
                v-model="kForm.content"
                :entry-id="kForm.id || 0"
                placeholder="像 Word 一样直接编辑：可粘贴截图、插入图片 / 文档 / 超链接 / 表格 / 代码块"
              />
            </div>
            <!-- 附件区：编辑现有条目显示正式附件；新建未保存时先进「中转缓存」，保存后转正 -->
            <div class="edit-attach">
              <div class="att-head">
                <span class="att-title">附件（{{ kForm.id ? kAtts.length : pendingAtts.length }}）</span>
                <div class="att-upload">
                  <span v-if="uploading" class="dim up-txt">上传中…</span>
                  <label class="btn sm">+ 上传文件
                    <input type="file" multiple :disabled="uploading" @change="uploadAtts" hidden />
                  </label>
                </div>
              </div>
              <p class="att-hint dim">{{ kForm.id ? '支持任意文件类型（图片可预览、PDF 可在线阅读），单文件 ≤ 100MB' : '编辑时即可上传；保存前文件暂存，保存后自动生效（单文件 ≤ 100MB）' }}</p>
              <div v-if="kForm.id && kAtts.length" class="att-list">
                <div v-for="a in kAtts" :key="a.id" class="att-item">
                  <img v-if="a.mime && a.mime.startsWith('image/')" :src="thumbUrl(a)" class="att-thumb" :alt="a.file_name" @click="previewAtt(a)" title="点击预览" />
                  <span v-else-if="isPdf(a)" class="att-ico pdf" @click="previewAtt(a)" :title="'在线阅读 ' + a.file_name">{{ fileIcon(a.file_name) }}</span>
                  <span v-else class="att-ico" @click="downloadAtt(a)" :title="'下载 ' + a.file_name">{{ fileIcon(a.file_name) }}</span>
                  <span class="att-name" :title="'下载 ' + a.file_name" @click="downloadAtt(a)">{{ a.file_name }}</span>
                  <button v-if="isPdf(a)" class="att-read" @click="previewAtt(a)">阅读</button>
                  <span class="att-size dim">{{ fmtSize(a.size) }}</span>
                  <button class="del danger" @click="removeAtt(a)">删除</button>
                </div>
              </div>
              <div v-else-if="kForm.id" class="empty att-empty">还没有附件</div>
              <div v-else-if="pendingAtts.length" class="att-list">
                <div v-for="a in pendingAtts" :key="a.tempId" class="att-item">
                  <span v-if="a.mime && a.mime.startsWith('image/')" class="att-thumb">{{ fileIcon(a.fileName) }}</span>
                  <span v-else class="att-ico">{{ fileIcon(a.fileName) }}</span>
                  <span class="att-name">{{ a.fileName }}</span>
                  <span class="att-size dim">{{ fmtSize(a.size) }}</span>
                  <span class="dim">待保存</span>
                  <button class="del danger" @click="removePendingAtt(a)">移除</button>
                </div>
              </div>
            </div>
            <div class="fg2 bot">
              <div class="scope-switch">
                <label class="fld">可见范围</label>
                <select v-model="kForm.scope" class="glass-input">
                  <option value="public" v-if="auth.isSuper">全公司可见</option>
                  <option value="department">同部门共享</option>
                  <option value="private">仅自己可见</option>
                </select>
              </div>
              <div class="form-actions">
                <button type="button" class="btn ghost" @click="cancelEditK">取消</button>
                <button type="submit" class="btn primary" :disabled="savingK">{{ savingK ? '保存中…' : (kForm.id ? '保存修改' : '保存') }}</button>
              </div>
            </div>
          </form>

          <!-- 筛选 -->
          <div v-else class="filter-bar">
            <input v-model="kQuery" class="glass-input search" placeholder="🔍 搜索标题 / 内容 / 分类 / 标签" @input="loadK" />
            <select v-model="kCategory" class="glass-input" @change="loadK">
              <option value="">全部分类</option>
              <option v-for="c in categories" :key="c" :value="c">{{ c }}</option>
            </select>
            <select v-model="kTag" class="glass-input" @change="loadK">
              <option value="">全部标签</option>
              <option v-for="t in allTags" :key="t" :value="t">{{ t }}</option>
            </select>
            <select v-model="kSort" class="glass-input" @change="loadK">
              <option value="recent">最近更新</option>
              <option value="created">最新创建</option>
              <option value="hot">最热（阅读量）</option>
            </select>
            <label class="chk mine"><input type="checkbox" v-model="kMine" @change="loadK" /> 只看我写的</label>
            <label class="chk mine"><input type="checkbox" v-model="kStarred" @change="loadK" /> 只看收藏</label>
            <button v-if="kParent" class="btn ghost sm" @click="kParent='';loadK()">目录：{{ parentTitle }} ✕</button>
          </div>

          <!-- 目录树（按 parent_id 组织） -->
          <div v-if="!editingK && kTree.length" class="k-tree" :class="{collapsed:kTreeOpen===false}">
            <div class="k-tree-head">
              <span class="dim">📁 目录</span>
              <button class="btn ghost sm" @click="kTreeOpen=!kTreeOpen">{{ kTreeOpen ? '收起' : '展开' }}</button>
            </div>
            <ul v-show="kTreeOpen" class="tree-ul">
              <li class="tree-li root"><span class="tree-node" :class="{active:kParent===''}" @click="kParent='';loadK()">📄 全部条目</span></li>
              <TreeNode v-for="n in kTree" :key="n.id" :node="n" :active="kParent" @pick="onPickParent" />
            </ul>
          </div>

          <!-- 列表 -->
          <div v-if="knowledge.length" class="k-grid">
            <div v-for="k in knowledge" :key="k.id" class="k-card" :class="{ mine: k.owner_id === auth.user?.id, pinned: k.pinned }">
              <div class="k-top">
                <span v-if="k.pinned" class="pin-badge" title="已置顶">📌</span>
                <span class="chip" :class="scopeChip(k.scope)">{{ scopeLabel(k.scope) }}</span>
                <span v-if="k.category" class="chip accent">{{ k.category }}</span>
                <span v-if="k.status==='draft'" class="chip warn">草稿</span>
                <span class="k-owner">{{ k.owner_name === (auth.user && auth.user.username) ? '我' : k.owner_name }}</span>
              </div>
              <div class="k-title" v-html="highlight(k.title, kQuery)" @click="openDetailK(k)"></div>
              <div class="k-preview" v-html="highlight(kPreview(k), kQuery)" @click="openDetailK(k)"></div>
              <div v-if="parseTags(k.tags).length" class="k-tags">
                <span v-for="t in parseTags(k.tags)" :key="t" class="k-tag" @click="kTag=t;loadK()">#{{ t }}</span>
              </div>
              <div class="k-foot">
                <span class="dim">{{ fmtTime(k.updated_at || k.created_at) }} · 👁 {{ k.view_count || 0 }}</span>
                <span class="k-foot-ops">
                  <button class="icon-btn" :class="{on:k.starred}" :title="k.starred?'取消收藏':'收藏'" @click.stop="toggleStar(k)">{{ k.starred ? '★' : '☆' }}</button>
                  <button v-if="k.owner_id===auth.user?.id || auth.isSuper" class="icon-btn" :class="{on:k.pinned}" :title="k.pinned?'取消置顶':'置顶'" @click.stop="togglePin(k)">{{ k.pinned ? '📌' : '📍' }}</button>
                  <span v-if="k.owner_id === auth.user?.id || auth.isSuper" class="ops">
                    <button class="del" @click="openEditK(k)">编辑</button>
                    <button class="del danger" @click="removeK(k)">删除</button>
                  </span>
                </span>
              </div>
            </div>
          </div>
          <div v-else class="empty">{{ kQuery || kTag || kParent ? '没有匹配的知识条目' : '还没有知识条目，点右上「新建」沉淀第一条吧' }}</div>
        </template>
      </section>

      <!-- 知识详情弹窗 -->
      <div v-if="viewK" class="modal-mask" @click.self="viewK = null">
        <div class="modal">
          <div class="modal-head">
            <span class="chip" :class="scopeChip(viewK.scope)">{{ scopeLabel(viewK.scope) }}</span>
            <span v-if="viewK.category" class="chip accent">{{ viewK.category }}</span>
            <span v-if="viewK.status==='draft'" class="chip warn">草稿</span>
            <span v-if="parseTags(viewK.tags).length" class="chip">{{ parseTags(viewK.tags).join('、') }}</span>
            <button class="icon-btn star" :class="{on:viewK.starred}" :title="viewK.starred?'取消收藏':'收藏'" @click="toggleStar(viewK)">{{ viewK.starred ? '★' : '☆' }}</button>
            <button v-if="viewK.owner_id===auth.user?.id || auth.isSuper" class="icon-btn pin" :class="{on:viewK.pinned}" :title="viewK.pinned?'取消置顶':'置顶'" @click="togglePin(viewK)">{{ viewK.pinned ? '📌' : '📍' }}</button>
            <button class="modal-close" @click="viewK = null">✕</button>
          </div>
          <h3 class="modal-title">{{ viewK.title }}</h3>
          <div class="modal-meta dim">
            {{ viewK.owner_name === (auth.user && auth.user.username) ? '我' : viewK.owner_name }} · 更新于 {{ fmtTime(viewK.updated_at || viewK.created_at) }} · 👁 {{ viewK.view_count || 0 }}
          </div>

          <!-- 子标签：正文 / 评论 / 版本 / 双链 -->
          <div class="k-tabs">
            <button class="k-tab" :class="{on:kTab==='content'}" @click="kTab='content'">正文</button>
            <button class="k-tab" :class="{on:kTab==='comments'}" @click="kTab='comments';loadComments(viewK.id)">评论 ({{ kComments.length }})</button>
            <button class="k-tab" :class="{on:kTab==='versions'}" @click="kTab='versions';loadVersions(viewK.id)">版本 ({{ kVersions.length }})</button>
            <button class="k-tab" :class="{on:kTab==='links'}" @click="kTab='links';loadLinks(viewK.id)">双向链接</button>
          </div>

          <div class="modal-scroll">
            <!-- 正文 -->
            <div v-show="kTab==='content'">
              <div class="modal-body rte-content" v-html="safeHtml(viewK.content) || '<span class=\'dim\'>（暂无内容）</span>'"></div>

              <!-- 附件区 -->
              <div v-if="viewK" class="modal-attach">
                <div class="att-head">
                  <span class="att-title">附件（{{ kAtts.length }}）</span>
                  <div v-if="canEditK(viewK)" class="att-upload">
                    <span v-if="uploading" class="dim up-txt">上传中…</span>
                    <label class="btn sm">+ 上传文件
                      <input type="file" multiple :disabled="uploading" @change="uploadAtts" hidden />
                    </label>
                  </div>
                </div>
                <p v-if="canEditK(viewK)" class="att-hint dim">支持任意文件类型（图片可预览、PDF 可在线阅读），单文件 ≤ 100MB</p>
                <div v-if="kAtts.length" class="att-list">
                  <div v-for="a in kAtts" :key="a.id" class="att-item">
                    <img v-if="a.mime && a.mime.startsWith('image/')" :src="thumbUrl(a)" class="att-thumb" :alt="a.file_name" @click="previewAtt(a)" title="点击预览" />
                    <span v-else-if="isPdf(a)" class="att-ico pdf" @click="previewAtt(a)" :title="'在线阅读 ' + a.file_name">{{ fileIcon(a.file_name) }}</span>
                    <span v-else class="att-ico" @click="downloadAtt(a)" :title="'下载 ' + a.file_name">{{ fileIcon(a.file_name) }}</span>
                    <span class="att-name" :title="'下载 ' + a.file_name" @click="downloadAtt(a)">{{ a.file_name }}</span>
                    <button v-if="isPdf(a)" class="att-read" @click="previewAtt(a)">阅读</button>
                    <span class="att-size dim">{{ fmtSize(a.size) }}</span>
                    <button v-if="canDelAtt(a)" class="del danger" @click="removeAtt(a)">删除</button>
                  </div>
                </div>
                <div v-else class="empty att-empty">还没有附件，可上传截图 / 文档等补充资料</div>
              </div>
            </div>

            <!-- 评论 -->
            <div v-show="kTab==='comments'" class="k-comments">
              <div v-if="!kComments.length" class="dim" style="padding:10px 0">还没有评论，来写第一条吧（可用 @姓名 提醒同事）</div>
              <div v-for="c in topComments" :key="c.id" class="comment">
                <div class="comment-head"><b>{{ c.user_name }}</b><span class="dim">{{ fmtTime(c.created_at) }}</span></div>
                <div class="comment-body" v-html="safeHtml(c.content)"></div>
                <div class="comment-ops">
                  <button class="del" @click="kReplyTo=c.id;kCommentText='@'+c.user_name+' '">回复</button>
                  <button v-if="c.user_id===auth.user?.id || auth.isSuper" class="del danger" @click="delComment(c.id)">删除</button>
                </div>
                <div v-for="r in repliesOf(c.id)" :key="r.id" class="comment reply">
                  <div class="comment-head"><b>{{ r.user_name }}</b><span class="dim">{{ fmtTime(r.created_at) }}</span></div>
                  <div class="comment-body" v-html="safeHtml(r.content)"></div>
                  <div class="comment-ops">
                    <button class="del danger" @click="delComment(r.id)">删除</button>
                  </div>
                </div>
              </div>
              <div class="comment-editor">
                <textarea v-model="kCommentText" class="glass-input ta" rows="2" :placeholder="kReplyTo?(kReplyTo?'回复中…':'') : '发表评论，@姓名 可提醒对方'"></textarea>
                <div class="comment-send">
                  <button v-if="kReplyTo" class="btn ghost sm" @click="kReplyTo=0;kCommentText=''">取消回复</button>
                  <button class="btn primary sm" :disabled="kCommentBusy" @click="addComment">{{ kCommentBusy ? '发送中…' : '发表评论' }}</button>
                </div>
              </div>
            </div>

            <!-- 版本历史 -->
            <div v-show="kTab==='versions'" class="k-versions">
              <div v-if="!kVersions.length" class="dim" style="padding:10px 0">暂无历史版本（每次保存会自动快照）</div>
              <div v-for="v in kVersions" :key="v.id" class="version-item">
                <div class="version-head"><span class="chip accent">v{{ v.version }}</span><b>{{ v.operator_name }}</b><span class="dim">{{ fmtTime(v.created_at) }}</span></div>
                <div class="version-meta dim">{{ v.title }} · {{ v.category || '未分类' }}</div>
                <button v-if="viewK.owner_id===auth.user?.id || auth.isSuper" class="btn ghost sm" @click="restoreVersion(v.id)">回滚到此版本</button>
              </div>
            </div>

            <!-- 双向链接 -->
            <div v-show="kTab==='links'" class="k-links">
              <div class="link-group">
                <div class="link-h">↗ 本条目引用的（出链）</div>
                <div v-if="!kOutlinks.length" class="dim">正文中使用 <code>[[标题]]</code> 语法即可建立引用</div>
                <div v-for="e in kOutlinks" :key="'o'+e.id" class="link-item" @click="openEntry(e)">{{ e.title }}</div>
              </div>
              <div class="link-group">
                <div class="link-h">↘ 引用本条目的（反向链接）</div>
                <div v-if="!kBacklinks.length" class="dim">还没有其它条目引用本条目</div>
                <div v-for="e in kBacklinks" :key="'b'+e.id" class="link-item" @click="openEntry(e)">{{ e.title }}</div>
              </div>
            </div>

            <!-- 变更 / 协作记录（v0.8.0） -->
            <div v-if="viewK" class="modal-history">
              <button class="btn ghost sm hist-toggle" @click="toggleKHist(viewK)">
                {{ kHistOpen ? '▾ 收起变更 / 协作记录' : '▸ 变更 / 协作记录（谁、何时、改了啥）' }}
              </button>
              <ul v-if="kHistOpen" class="hist-list">
                <li v-if="!kHist.length" class="dim hist-empty">暂无变更记录</li>
                <li v-for="h in kHist" :key="h.id" class="hist-item">
                  <div class="hist-head">
                    <span class="chip accent hist-act">{{ histActionLabel(h.action) }}</span>
                    <b>{{ h.operator_name }}</b>
                    <span class="dim">{{ fmtTime(h.created_at) }}</span>
                  </div>
                  <pre v-if="h.detail" class="hist-detail">{{ h.detail }}</pre>
                </li>
              </ul>
            </div>
          </div>

          <div class="modal-foot">
            <button v-if="viewK.owner_id === auth.user?.id" class="btn ghost" @click="editFromView">编辑</button>
            <button class="btn primary" @click="viewK = null">关闭</button>
          </div>
        </div>
      </div>

      <!-- 模板选择弹窗 -->
      <div v-if="templateModal" class="modal-mask" @click.self="templateModal=false">
        <div class="modal sm">
          <div class="modal-head">
            <h3 class="modal-title">从模板新建</h3>
            <button class="modal-close" @click="templateModal=false">✕</button>
          </div>
          <div class="modal-scroll">
            <div v-if="!templates.length" class="dim" style="padding:10px 0">暂无模板</div>
            <div v-for="t in templates" :key="t.id" class="tpl-item">
              <div class="tpl-main">
                <span class="tpl-title">{{ t.title }}</span>
                <span class="dim">{{ t.category || '通用' }} · <span v-if="t.builtin">系统预置</span><span v-else>我的</span></span>
              </div>
              <div class="tpl-ops">
                <button class="btn sm primary" @click="applyTemplate(t)">使用</button>
                <button v-if="!t.builtin" class="btn sm danger" @click="deleteTemplate(t)">删除</button>
              </div>
            </div>
          </div>
          <div class="modal-foot">
            <button class="btn ghost" @click="templateModal=false">关闭</button>
          </div>
        </div>
      </div>

      <!-- 导入弹窗 -->
      <div v-if="importModal" class="modal-mask" @click.self="importModal=false">
        <div class="modal sm">
          <div class="modal-head">
            <h3 class="modal-title">导入 Markdown</h3>
            <button class="modal-close" @click="importModal=false">✕</button>
          </div>
          <div class="modal-scroll">
            <p class="dim" style="font-size:12px">按 <code># 一级标题</code> 切分为多条；正文会被转换为富文本。</p>
            <textarea v-model="importText" class="glass-input ta" rows="10" placeholder="# 标题一&#10;正文内容…&#10;&#10;# 标题二&#10;另一篇…"></textarea>
            <div class="fg2" style="margin-top:10px">
              <div>
                <label class="fld">默认分类</label>
                <input v-model="importCategory" class="glass-input" placeholder="可留空" />
              </div>
              <div>
                <label class="fld">可见范围</label>
                <select v-model="importScope" class="glass-input">
                  <option value="department">同部门共享</option>
                  <option value="private">仅自己可见</option>
                </select>
              </div>
            </div>
          </div>
          <div class="modal-foot">
            <button class="btn ghost" @click="importModal=false">取消</button>
            <button class="btn primary" :disabled="importing" @click="doImport">{{ importing ? '导入中…' : '开始导入' }}</button>
          </div>
        </div>
      </div>
    </template>

    <!-- ========== 工作日志 ========== -->
    <template v-if="tab === 'logs'">
      <section class="panel">
        <div class="panel-head">
          <h3 class="section-title">工作日志
            <span class="section-sub">记录当天做了什么、还剩什么没做完</span>
          </h3>
          <div class="head-actions">
            <button class="btn ghost sm" @click="logStatsOpen = !logStatsOpen">{{ logStatsOpen ? '收起统计' : '📊 统计' }}</button>
            <button class="btn ghost sm" @click="exportLogs" title="按当前筛选条件导出 CSV（Excel 可直接打开）">⬆ 导出</button>
            <button v-if="!editingLog" class="btn primary" @click="openNewLog">+ 写一篇</button>
            <button v-else class="btn ghost" @click="cancelEditLog">收起</button>
          </div>
        </div>

        <!-- 统计看板 -->
        <div v-if="logStatsOpen" class="ws-stats">
          <div class="ws-stat"><b>{{ logStats?.total ?? '—' }}</b><span>篇（当前筛选）</span></div>
          <div class="ws-stat"><b>{{ logStats?.people ?? '—' }}</b><span>人参与</span></div>
          <div class="ws-stat"><b>{{ logStats?.with_pending ?? '—' }}</b><span>含未完成事项</span></div>
          <div class="ws-stat"><b>{{ logStats?.edited ?? '—' }}</b><span>被编辑过</span></div>
          <div class="ws-stat" :class="{ warn: (logStats?.missing_today?.length || 0) > 0 }">
            <b>{{ logStats?.missing_today?.length ?? 0 }}</b><span>今日未填写</span>
          </div>
          <button v-if="logStats?.missing_today?.length" class="btn ghost sm" @click="logMissingOpen = !logMissingOpen">
            {{ logMissingOpen ? '收起名单' : '查看名单' }}
          </button>
        </div>
        <div v-if="logStatsOpen && logMissingOpen && logStats?.missing_today?.length" class="ws-missing">
          <span class="dim">今日（{{ logStats.today }}）尚未填写：</span>
          <span v-for="m in logStats.missing_today" :key="m.user_id" class="chip miss">{{ m.name }}<template v-if="m.emp_no"> · {{ m.emp_no }}</template></span>
        </div>

        <!-- 筛选栏 -->
        <div class="ws-filters">
          <label class="ff"><span>从</span><input type="date" v-model="logFrom" class="glass-input date-input" @change="applyLogFilter" /></label>
          <label class="ff"><span>到</span><input type="date" v-model="logTo" class="glass-input date-input" @change="applyLogFilter" /></label>
          <input v-model="logKw" class="glass-input ff-grow" placeholder="搜索主题 / 已做 / 未完成" @keyup.enter="applyLogFilter" />
          <select v-model="logOwner" class="glass-input ff-owner" @change="applyLogFilter">
            <option value="">全部记录人</option>
            <option v-for="u in pickableUsers" :key="u.id" :value="u.id">{{ u.name }}</option>
          </select>
          <div class="sub-tabs">
            <button class="seg" :class="{ on: logScope === 'mine' }" @click="setLogScope('mine')">我写的</button>
            <button class="seg" :class="{ on: logScope === 'dept' }" @click="setLogScope('dept')">部门共享</button>
          </div>
          <button class="btn ghost sm" @click="resetLogFilters">重置</button>
        </div>

        <!-- 内联写日志 -->
        <form v-if="editingLog" class="edit-form" @submit.prevent="saveLog">
          <div class="fg2">
            <div>
              <label class="fld">日期 *</label>
              <input v-model="logForm.log_date" type="date" class="glass-input" required />
            </div>
            <div>
              <label class="fld">本篇主题</label>
              <input v-model="logForm.title" class="glass-input" placeholder="如：早班开档 / 周年庆活动跟进" />
            </div>
          </div>
          <div>
            <label class="fld"><b style="color:var(--ok)">✅ 今天做了什么</b></label>
            <textarea v-model="logForm.done" class="glass-input ta" rows="3" placeholder="逐条写下今天完成的事项"></textarea>
          </div>
          <div>
            <label class="fld"><b style="color:var(--warn)">⏳ 还没做完的 / 待办遗留</b></label>
            <textarea v-model="logForm.pending" class="glass-input ta" rows="2" placeholder="哪些还没做完，可一键转成交接单交给下一个人"></textarea>
          </div>
          <div class="fg2 bot">
            <div class="scope-switch">
              <label class="fld">可见范围</label>
              <select v-model="logForm.scope" class="glass-input">
                <option value="private">仅自己可见</option>
                <option value="department">同部门共享</option>
              </select>
            </div>
            <div class="form-actions">
              <button type="button" class="btn ghost" @click="cancelEditLog">取消</button>
              <button type="submit" class="btn primary" :disabled="savingLog">{{ savingLog ? '保存中…' : (logForm.id ? '保存修改' : '保存') }}</button>
            </div>
          </div>
        </form>

        <!-- 日志列表 -->
        <div v-if="logs.length" class="log-list">
          <div v-for="l in logs" :key="l.id" class="log-card">
            <div class="log-head">
              <span class="log-time">{{ l.log_date }}</span>
              <span v-if="l.title" class="log-title">{{ l.title }}</span>
              <span class="chip" :class="scopeChip(l.scope)">{{ scopeLabel(l.scope) }}</span>
              <span class="dim owner">{{ l.owner_name === (auth.user && auth.user.username) ? '我' : l.owner_name }}</span>
              <span v-if="l.edit_count" class="chip tiny" :title="'最后修改：' + (l.last_editor_name || '') + ' · ' + fmtTime(l.updated_at)">已编辑 {{ l.edit_count }} 次</span>
              <span v-if="l.attach_count" class="chip tiny">📎 {{ l.attach_count }}</span>
              <span v-if="l.handover_count" class="chip tiny ok">已转交接</span>
              <span class="ops">
                <button class="del" @click="toggleLogAttach(l)">{{ logAttachOpen === l.id ? '收起附件' : '📎 附件' }}</button>
                <button v-if="l.pending && l.owner_id === auth.user?.id" class="del" @click="openLogHandover(l)" title="把「还没做完的」一键转成交接单">转交接</button>
                <template v-if="l.owner_id === auth.user?.id">
                  <button class="del" @click="openEditLog(l)">编辑</button>
                  <button class="del danger" @click="removeLog(l)">删除</button>
                </template>
              </span>
            </div>
            <div v-if="l.done" class="log-sec done">
              <span class="sec-label">今天做了什么</span>{{ l.done }}
            </div>
            <div v-if="l.pending" class="log-sec pend">
              <span class="sec-label">还没做完的</span>{{ l.pending }}
            </div>
            <div v-if="!l.done && !l.pending" class="dim">（本篇暂无内容）</div>

            <!-- 附件区（展开式，按需加载） -->
            <div v-if="logAttachOpen === l.id" class="attach-box">
              <div v-if="logAttachList.length" class="attach-list">
                <div v-for="a in logAttachList" :key="a.id" class="attach-row">
                  <span class="att-ico" :title="a.file_name">{{ fileIcon(a.file_name) }}</span>
                  <span class="attach-name" @click="downloadWSAtt(a)" :title="'下载 ' + a.file_name">{{ a.file_name }}</span>
                  <span class="dim">{{ fmtSize(a.size) }}</span>
                  <span class="dim">{{ a.owner_name }}</span>
                  <button class="del danger" @click="removeWSAtt(a, 'log', l.id)">删除</button>
                </div>
              </div>
              <div v-else class="dim">暂无附件</div>
              <label class="btn ghost sm file-btn">
                {{ uploading ? '上传中…' : '＋ 上传附件' }}
                <input type="file" :disabled="uploading" hidden @change="(e) => uploadWSAtt(e, 'log', l.id)" />
              </label>
            </div>
          </div>
        </div>
        <div v-else class="empty">没有符合条件的日志，换个筛选条件或点「写一篇」记录今天的工作</div>

        <!-- 分页 -->
        <div v-if="logTotal > logLimit" class="pager">
          <button class="btn ghost sm" :disabled="logOffset === 0" @click="logPage(-1)">← 上一页</button>
          <span class="dim">第 {{ Math.floor(logOffset / logLimit) + 1 }} / {{ Math.ceil(logTotal / logLimit) }} 页 · 共 {{ logTotal }} 篇</span>
          <button class="btn ghost sm" :disabled="logOffset + logLimit >= logTotal" @click="logPage(1)">下一页 →</button>
        </div>
      </section>

      <!-- 日志 → 交接单：把「还没做完的」一键转出去 -->
      <div v-if="logHvTarget" class="modal-mask" @click.self="cancelLogHandover">
        <div class="modal sm">
          <div class="modal-head">
            <span class="modal-title" style="margin:0">把「还没做完的」转成交接单</span>
            <button class="modal-close" @click="cancelLogHandover" aria-label="关闭">×</button>
          </div>
          <div class="modal-meta dim">
            源自 {{ logHvTarget.log_date }} 的日志「{{ logHvTarget.title || '（无主题）' }}」，转出后接收人会收到通知。
          </div>
          <div class="modal-scroll">
            <div class="hv-form">
              <div>
                <label class="fld">交接标题 *</label>
                <input v-model="logHvForm.title" class="glass-input" />
              </div>
              <div>
                <label class="fld">需要接收人继续做的事 *</label>
                <textarea v-model="logHvForm.todo" class="glass-input ta" rows="3" placeholder="默认取日志里「还没做完的」内容，可修改"></textarea>
              </div>
              <div>
                <label class="fld">接收人 * <span class="dim" style="font-weight:normal">（可多选）</span></label>
                <div class="multi-pick">
                  <div v-if="logHvForm.assignee_ids.length" class="chips">
                    <span v-for="uid in logHvForm.assignee_ids" :key="uid" class="chip pick-chip">
                      {{ userNameOf(uid) }}<button type="button" class="x" @click="toggleAssigneeIn(logHvForm.assignee_ids, uid)" aria-label="移除">×</button>
                    </span>
                  </div>
                  <button type="button" class="btn ghost sm" @click="logHvPicker = !logHvPicker">{{ logHvPicker ? '收起选择' : '+ 添加接收人' }}</button>
                  <div v-if="logHvPicker" class="picker-list">
                    <label v-for="u in pickableUsers" :key="u.id" class="picker-row">
                      <input type="checkbox" :checked="logHvForm.assignee_ids.includes(u.id)" @change="toggleAssigneeIn(logHvForm.assignee_ids, u.id)" />
                      <span class="picker-name">{{ u.name }}<span v-if="u.dept" class="dim">（{{ u.dept.name }}）</span></span>
                    </label>
                    <div v-if="!pickableUsers.length" class="dim picker-empty">没有可选的接收人</div>
                  </div>
                </div>
              </div>
              <div>
                <label class="fld">优先级与截止时间</label>
                <div class="pri-row">
                  <select v-model="logHvForm.priority" class="glass-input">
                    <option value="normal">普通</option>
                    <option value="urgent">紧急</option>
                  </select>
                  <input type="datetime-local" v-model="logHvForm.due_at" class="glass-input" title="期望完成时间（可留空）" />
                </div>
              </div>
              <div>
                <label class="fld">交接说明（可选）</label>
                <input v-model="logHvForm.note" class="glass-input" placeholder="补充背景，会追加到「当前进展」" />
              </div>
            </div>
          </div>
          <div class="modal-foot">
            <button class="btn ghost" @click="cancelLogHandover">取消</button>
            <button class="btn primary" :disabled="savingLogHv" @click="saveLogHandover">{{ savingLogHv ? '提交中…' : '转成交接单' }}</button>
          </div>
        </div>
      </div>
    </template>

    <!-- ========== 交接接力 ========== -->
    <template v-if="tab === 'handover'">
      <section class="panel">
        <div class="panel-head">
          <h3 class="section-title">交接接力 <span class="section-sub">把做到哪、接下来做什么清楚转给下一个人</span></h3>
          <div class="head-actions">
            <div class="sub-tabs">
              <button class="seg" :class="{ on: hScope === 'inbox' }" @click="setHScope('inbox')">我收到的<template v-if="inboxUnread"> ({{ inboxUnread }})</template></button>
              <button class="seg" :class="{ on: hScope === 'outbox' }" @click="setHScope('outbox')">我发出的</button>
            </div>
            <button v-if="!editingH" class="btn primary" @click="openNewH">+ 发起交接</button>
            <button v-else class="btn ghost" @click="cancelEditH">返回列表</button>
          </div>
        </div>

        <!-- 统计卡片 -->
        <div class="stat-cards">
          <button class="stat-card" :class="{ on: hStatus === 'all' }" @click="setHFilter('status', 'all')">
            <b>{{ hStats?.total ?? 0 }}</b><span>全部</span>
          </button>
          <button class="stat-card warn" :class="{ on: hStatus === 'pending' }" @click="setHFilter('status', 'pending')">
            <b>{{ hStats?.pending ?? 0 }}</b><span>待接手</span>
          </button>
          <button class="stat-card accent" :class="{ on: hStatus === 'in_progress' }" @click="setHFilter('status', 'in_progress')">
            <b>{{ hStats?.in_progress ?? 0 }}</b><span>处理中</span>
          </button>
          <button class="stat-card ok" :class="{ on: hStatus === 'done' }" @click="setHFilter('status', 'done')">
            <b>{{ hStats?.done ?? 0 }}</b><span>已完成</span>
          </button>
          <button class="stat-card" :class="{ on: hStatus === 'returned' }" @click="setHFilter('status', 'returned')">
            <b>{{ hStats?.returned ?? 0 }}</b><span>已退回</span>
          </button>
          <button class="stat-card danger" :class="{ on: hPriority === 'urgent' }" @click="setHFilter('priority', hPriority === 'urgent' ? '' : 'urgent')">
            <b>{{ hStats?.urgent ?? 0 }}</b><span>紧急未完成</span>
          </button>
          <button class="stat-card danger" :class="{ on: hOverdueOnly }" @click="hOverdueOnly = !hOverdueOnly; applyHFilter()">
            <b>{{ hOverdueOnly ? '只看' : (hStats?.overdue ?? 0) }}</b><span>已逾期</span>
          </button>
        </div>

        <!-- 筛选栏 -->
        <div class="ws-filters">
          <input v-model="hKw" class="glass-input ff-grow" placeholder="搜索标题 / 进展 / 待办" @keyup.enter="applyHFilter" />
          <select v-model="hStatus" class="glass-input" @change="applyHFilter">
            <option value="all">全部状态</option>
            <option value="pending">待接手</option>
            <option value="in_progress">处理中</option>
            <option value="done">已完成</option>
            <option value="returned">已退回</option>
          </select>
          <select v-model="hPriority" class="glass-input" @change="applyHFilter">
            <option value="">全部优先级</option>
            <option value="urgent">仅紧急</option>
            <option value="normal">仅普通</option>
          </select>
          <label class="ff chk"><input type="checkbox" v-model="hOverdueOnly" @change="applyHFilter" /><span>仅看逾期</span></label>
          <button class="btn ghost sm" @click="resetHFilters">重置</button>
        </div>

        <!-- 新建交接 -->
        <form v-if="editingH" class="edit-form" @submit.prevent="saveH">
          <div>
            <label class="fld">交接标题 *</label>
            <input v-model="hForm.title" class="glass-input" required placeholder="如：周年庆小红书素材整理（未完成部分）" />
          </div>
          <div class="fg2">
            <div>
              <label class="fld">接收人 * <span class="dim" style="font-weight:normal">（可多选）</span></label>
              <div class="multi-pick">
                <div v-if="hForm.assignee_ids.length" class="chips">
                  <span v-for="uid in hForm.assignee_ids" :key="uid" class="chip pick-chip">
                    {{ userNameOf(uid) }}<button type="button" class="x" @click="toggleAssignee(uid)" aria-label="移除">×</button>
                  </span>
                </div>
                <button type="button" class="btn ghost sm" @click="showPicker = !showPicker">{{ showPicker ? '收起选择' : '+ 添加接收人' }}</button>
                <div v-if="showPicker" class="picker-list">
                  <label v-for="u in pickableUsers" :key="u.id" class="picker-row">
                    <input type="checkbox" :checked="hForm.assignee_ids.includes(u.id)" @change="toggleAssignee(u.id)" />
                    <span class="picker-name">{{ u.name }}<span v-if="u.dept" class="dim">（{{ u.dept.name }}）</span></span>
                  </label>
                  <div v-if="!pickableUsers.length" class="dim picker-empty">没有可选的接收人</div>
                </div>
              </div>
            </div>
            <div>
              <label class="fld">优先级与截止时间</label>
              <div class="pri-row">
                <select v-model="hForm.priority" class="glass-input">
                  <option value="normal">普通</option>
                  <option value="urgent">紧急</option>
                </select>
                <input type="datetime-local" v-model="hForm.due_at" class="glass-input" title="期望完成时间（可留空）" />
              </div>
            </div>
          </div>
          <div>
            <label class="fld"><b style="color:var(--accent)">📌 我做到哪了（当前进展）</b></label>
            <textarea v-model="hForm.from_progress" class="glass-input ta" rows="2" placeholder="已完成/进行到哪一步，让接收人快速上手"></textarea>
          </div>
          <div>
            <label class="fld"><b style="color:var(--warn)">🔜 需要接收人继续做的事</b></label>
            <textarea v-model="hForm.todo" class="glass-input ta" rows="3" placeholder="还没做完的，需要下一个人继续处理的事项"></textarea>
          </div>
          <div class="fg2 bot">
            <div class="form-actions right">
              <button type="button" class="btn ghost" @click="cancelEditH">取消</button>
              <button type="submit" class="btn primary" :disabled="savingH">{{ savingH ? '发出中…' : '发出交接' }}</button>
            </div>
          </div>
        </form>

        <!-- 交接列表 -->
        <template v-else>
          <div v-if="handovers.length" class="h-list">
            <div v-for="h in handovers" :key="h.id" class="h-card" :class="'st-' + h.status">
              <div class="h-head">
                <span v-if="h.priority === 'urgent'" class="chip danger">紧急</span>
                <span class="chip" :class="statusChip(h.status)">{{ statusLabel(h.status) }}</span>
                <span class="h-title">{{ h.title }}</span>
                <span class="dim">#{{ h.id }}</span>
                <span v-if="h.overdue" class="chip danger" :title="'已超过期望完成时间 ' + fmtTime(h.due_at)">已逾期</span>
                <span v-else-if="h.due_at && h.status !== 'done'" class="chip tiny" :title="'期望完成时间'">⏰ {{ fmtTime(h.due_at) }}</span>
                <span v-if="h.attach_count" class="chip tiny">📎 {{ h.attach_count }}</span>
                <span v-if="h.urge_count" class="chip tiny warn" :title="'最近催办 ' + fmtTime(h.last_urge_at)">已催办 {{ h.urge_count }} 次</span>
                <span class="ops">
                  <button class="del" @click="toggleHTimeline(h)">{{ hTimelineOpen === h.id ? '收起轨迹' : '处理轨迹' }}<template v-if="h.event_count"> ({{ h.event_count }})</template></button>
                  <button class="del" @click="toggleHAttach(h)">{{ hAttachOpen === h.id ? '收起附件' : '📎 附件' }}</button>
                  <button v-if="isSenderOf(h) && h.status !== 'done'" class="del" @click="urgeH(h)" title="提醒接收人尽快处理">催办</button>
                  <button v-if="h.sender_id === auth.user?.id" class="del danger" @click="removeH(h)">删除</button>
                </span>
              </div>
              <div class="h-meta dim">
                {{ hScope === 'inbox' ? '来自' : '发给' }} <b>{{ hScope === 'inbox' ? h.sender_name : assigneeNamesText(h) }}</b>
                <template v-if="hScope === 'outbox' && parseAssigneeNames(h).length > 1">（{{ parseAssigneeNames(h).length }} 人）</template>
                · {{ fmtTime(h.created_at) }}
                <template v-if="h.accepted_at && h.status !== 'pending'"> · {{ h.accepted_by_name || '接收人' }} 于 {{ fmtTime(h.accepted_at) }} 接手</template>
                <template v-if="h.status === 'done'"> · 完成于 {{ fmtTime(h.completed_at) }}</template>
              </div>
              <div v-if="h.from_progress" class="h-prog">
                <span class="sec-label">当前进展</span>{{ h.from_progress }}
              </div>
              <div v-if="h.todo" class="h-todo">
                <span class="sec-label">待继续</span>{{ h.todo }}
              </div>
              <div v-if="h.status === 'returned'" class="h-note ret-note">
                <span class="sec-label">退回原因</span><b>{{ h.return_reason || '（未填写）' }}</b>
              </div>
              <div v-if="h.status === 'in_progress'" class="h-note">
                <span class="sec-label">接收人备注</span><span class="dim">{{ h.note || '（处理中，暂无备注）' }}</span>
              </div>
              <div v-if="h.status === 'done' && h.note" class="h-note done-note">
                <span class="sec-label">完成说明</span>{{ h.note }}
              </div>

              <!-- 处理轨迹 -->
              <div v-if="hTimelineOpen === h.id" class="timeline">
                <div v-if="hTimeline.length" class="tl-list">
                  <div v-for="e in hTimeline" :key="e.id" class="tl-item" :class="'tl-' + e.action">
                    <span class="tl-dot"></span>
                    <span class="tl-act">{{ handoverActionLabel(e.action) }}</span>
                    <span class="tl-who">{{ e.actor_name }}</span>
                    <span class="tl-time dim">{{ fmtTime(e.created_at) }}</span>
                    <span v-if="e.note" class="tl-note">{{ e.note }}</span>
                  </div>
                </div>
                <div v-else class="dim">暂无处理轨迹</div>
              </div>

              <!-- 附件 -->
              <div v-if="hAttachOpen === h.id" class="attach-box">
                <div v-if="hAttachList.length" class="attach-list">
                  <div v-for="a in hAttachList" :key="a.id" class="attach-row">
                    <span class="att-ico">{{ fileIcon(a.file_name) }}</span>
                    <span class="attach-name" @click="downloadWSAtt(a)" :title="'下载 ' + a.file_name">{{ a.file_name }}</span>
                    <span class="dim">{{ fmtSize(a.size) }}</span>
                    <span class="dim">{{ a.owner_name }}</span>
                    <button class="del danger" @click="removeWSAtt(a, 'handover', h.id)">删除</button>
                  </div>
                </div>
                <div v-else class="dim">暂无附件</div>
                <label class="btn ghost sm file-btn">
                  {{ uploading ? '上传中…' : '＋ 上传附件' }}
                  <input type="file" :disabled="uploading" hidden @change="(e) => uploadWSAtt(e, 'handover', h.id)" />
                </label>
              </div>

              <!-- 操作区：任一接收人可推进 -->
              <div v-if="isAssigneeOf(h) && h.status !== 'done'" class="h-actions">
                <template v-if="h.status === 'pending' || h.status === 'returned'">
                  <button class="btn sm ok" @click="setHStatus(h, 'in_progress')">👌 接手处理</button>
                </template>
                <template v-if="h.status === 'in_progress' || h.status === 'pending'">
                  <button class="btn sm primary" @click="promptDone(h)">✅ 标记完成</button>
                </template>
                <button class="btn sm ghost" @click="promptReturn(h)">↩ 退回</button>
                <button v-if="h.status === 'in_progress'" class="btn sm" @click="promptNote(h)">✍️ 更新进度备注</button>
              </div>
              <!-- 发送人：退回后重新指派/撤单 -->
              <div v-if="isSenderOf(h) && h.status === 'returned'" class="h-actions">
                <button class="btn sm" @click="reopenH(h)">🔄 重新派发（回到待接手）</button>
              </div>
            </div>
          </div>
          <div v-else class="empty">
            {{ hScope === 'inbox' ? '没有符合条件的交接' : '你还没发起过符合条件的交接' }}，点右上「发起交接」把未完成的事交给下一个人
          </div>

          <!-- 分页 -->
          <div v-if="hTotal > hLimit" class="pager">
            <button class="btn ghost sm" :disabled="hOffset === 0" @click="hPage(-1)">← 上一页</button>
            <span class="dim">第 {{ Math.floor(hOffset / hLimit) + 1 }} / {{ Math.ceil(hTotal / hLimit) }} 页 · 共 {{ hTotal }} 条</span>
            <button class="btn ghost sm" :disabled="hOffset + hLimit >= hTotal" @click="hPage(1)">下一页 →</button>
          </div>
        </template>
      </section>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useAutoRefresh } from '@/autoRefresh'
import * as api from '@/api'
// 直取 axios 实例：交接列表需要读 X-Total-Count 响应头做分页（api.get 只返回 body）
import http from '@/api/http'
import { useAuthStore } from '@/store/auth'
import { getCurrentInstance } from 'vue'
import RichTextEditor from '@/components/RichTextEditor.vue'

// 渲染前净化（纵深防御，与后端 sanitizeRichContent 同口径）防存储型 XSS
function safeHtml(html) {
  if (!html) return ''
  let s = String(html)
  for (const t of ['script', 'style', 'iframe', 'object', 'embed', 'link', 'meta', 'form', 'svg', 'math']) {
    s = s.replace(new RegExp('<\\s*' + t + '\\b[^>]*>[\\s\\S]*?<\\s*/\\s*' + t + '\\s*>', 'gi'), '')
    s = s.replace(new RegExp('<\\s*' + t + '\\b[^>]*/?>', 'gi'), '')
  }
  s = s.replace(/\son\w+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/gi, '')
  s = s.replace(/(href|src|xlink:href|action|formaction)\s*=\s*(?:"|')?\s*(?:javascript|vbscript|data)\s*:/gi, '')
  s = s.replace(/\sstyle\s*=\s*(?:"[^"]*"|'[^']*')/gi, '')
  return s
}

const { proxy } = getCurrentInstance()
const $msg = proxy?.$msg
const auth = useAuthStore()

const tab = ref('knowledge')

// ---------- 通用 ----------
function toast(t, type = 'success') { $msg ? $msg[type](t) : alert(t) }
function todayStr() {
  const d = new Date()
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d)) return String(s).slice(0, 16)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
const scopeLabel = (s) => (s === 'private' ? '仅自己' : '同部门共享')
const scopeChip = (s) => (s === 'private' ? '' : 'accent')
function switchTab(t) { tab.value = t }

// ================= 迷你知识库 =================
const knowledge = ref([])
const categories = ref([])
const allTags = ref([])
const kbView = ref('list') // list | stats | trash
const exportMenu = ref(false) // 导出下拉菜单开关
// 视图切换（替代原先散落的 toggleStats/openTrash，避免语义与状态不一致）
function kbGo(v) {
  if (kbView.value === v) return
  kbView.value = v
  exportMenu.value = false
  if (v === 'stats') loadStats()
  else if (v === 'trash') loadTrash()
}
function closeExportMenu() { exportMenu.value = false }
// 导出下拉：选中后先收起菜单再执行，避免菜单浮在下载提示之上
function pickExport(fn) { exportMenu.value = false; fn() }
function onKbDocClick() { if (exportMenu.value) exportMenu.value = false }
const kQuery = ref('')
const kCategory = ref('')
const kTag = ref('')
const kParent = ref('') // '' = 全部；'root' = 仅顶级；否则父级 id 字符串
const kSort = ref('recent') // recent | created | hot
const kMine = ref(false)
const kStarred = ref(false)
const editingK = ref(false)
const savingK = ref(false)
const viewK = ref(null)
const kForm = reactive({ id: 0, title: '', category: '', content: '', scope: 'department', tags: [], parent_id: 0, status: 'published' })
const kTagDraft = ref('')

let kTimer = null
async function loadK() {
  clearTimeout(kTimer)
  kTimer = setTimeout(async () => {
    try {
      const params = {}
      if (kQuery.value.trim()) params.kw = kQuery.value.trim()
      if (kCategory.value) params.category = kCategory.value
      if (kTag.value) params.tag = kTag.value
      if (kParent.value) params.parent = kParent.value
      if (kMine.value) params.mine = 1
      if (kStarred.value) params.starred = 1
      if (kSort.value && kSort.value !== 'recent') params.sort = kSort.value
      knowledge.value = await api.get('/workspace/knowledge', params)
    } catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
  }, (kMine.value || kCategory.value || kTag.value || kParent.value) ? 0 : 250)
}
async function loadCats() {
  try { categories.value = await api.get('/workspace/knowledge/categories') } catch {}
}
async function loadTags() {
  try { allTags.value = await api.get('/workspace/knowledge/tags') } catch {}
}
// 统一刷新（列表 + 分类 + 标签）
async function refreshKnowledge() {
  await Promise.all([loadK(), loadCats(), loadTags()])
}
function parseTags(s) {
  if (!s) return []
  s = String(s).trim()
  if (s.startsWith('[')) {
    try { const a = JSON.parse(s); if (Array.isArray(a)) return a.map((x) => String(x).trim()).filter(Boolean) } catch {}
  }
  return s.split(',').map((x) => x.trim()).filter(Boolean)
}
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}
// 搜索高亮（先转义，再包裹匹配片段，避免破坏 HTML）
function highlight(text, kw) {
  const raw = (text || '').toString()
  const safe = escapeHtml(raw)
  const k = (kw || '').trim()
  if (!k) return safe
  const esc = k.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return safe.replace(new RegExp('(' + esc + ')', 'gi'), '<mark>$1</mark>')
}
// 纯文本预览（用于卡片与高亮）
function kPreview(k) {
  const raw = (k && k.content) || ''
  const tmp = document.createElement('div')
  tmp.innerHTML = raw
  const text = (tmp.innerText || '').trim()
  return text ? (text.length > 160 ? text.slice(0, 160) + '…' : text) : '（暂无内容）'
}

// 目录树（按 parent_id 构建）
const kTreeOpen = ref(true)
const kTree = computed(() => {
  const items = knowledge.value || []
  const map = {}
  items.forEach((k) => { map[k.id] = { ...k, children: [] } })
  const roots = []
  items.forEach((k) => {
    const node = map[k.id]
    const pid = k.parent_id || 0
    if (pid && map[pid]) map[pid].children.push(node)
    else roots.push(node)
  })
  return roots
})
function onPickParent(id) { kParent.value = String(id); kbView.value = 'list'; loadK() }
const parentTitle = computed(() => {
  if (!kParent.value) return ''
  const hit = (knowledge.value || []).find((x) => String(x.id) === String(kParent.value))
  return hit ? hit.title : ''
})
const parentCandidates = computed(() => (knowledge.value || []).filter((x) => x.id !== kForm.id))

function addKTag() {
  const t = kTagDraft.value.trim()
  if (t && !kForm.tags.includes(t)) kForm.tags.push(t)
  kTagDraft.value = ''
}

function openNewK() {
  Object.assign(kForm, { id: 0, title: '', category: '', content: '', scope: 'department', tags: [], parent_id: 0, status: 'published' })
  kTagDraft.value = ''
  kAtts.value = []
  pendingAtts.value = []
  editingK.value = true
}
function openEditK(k) {
  Object.assign(kForm, { id: k.id, title: k.title, category: k.category, content: k.content, scope: k.scope, tags: parseTags(k.tags), parent_id: k.parent_id || 0, status: k.status || 'published' })
  kTagDraft.value = ''
  editingK.value = true
  loadKAtts(k.id)
}
function cancelEditK() { editingK.value = false; kAtts.value = []; pendingAtts.value = [] }
async function saveK() {
  savingK.value = true
  try {
    let savedId = kForm.id
    const payload = { title: kForm.title, category: kForm.category, content: kForm.content, scope: kForm.scope, tags: kForm.tags, parent_id: kForm.parent_id, status: kForm.status }
    if (kForm.id) {
      await api.put('/workspace/knowledge/' + kForm.id, payload)
      toast('已保存')
    } else {
      if (pendingAtts.value.length) payload.temp_attachment_ids = pendingAtts.value.map((a) => a.tempId)
      const created = await api.post('/workspace/knowledge', payload)
      savedId = created && created.id ? created.id : null
      if (created && created.content) kForm.content = created.content
      pendingAtts.value = []
      toast('已创建，正在打开详情…')
    }
    await refreshKnowledge()
    if (savedId && !kForm.id) {
      const fresh = (knowledge.value || []).find((x) => x.id === savedId)
      editingK.value = false
      if (fresh) openDetailK(fresh)
    }
  } catch (e) { toast(e.response?.data?.error || '保存失败', 'error') }
  finally { savingK.value = false }
}
async function openDetailK(k) {
  try {
    const d = await api.get('/workspace/knowledge/' + k.id)
    viewK.value = d
    kHistOpen.value = false
    kHist.value = []
    kTab.value = 'content'
    kComments.value = []
    kVersions.value = []
    kOutlinks.value = []
    kBacklinks.value = []
    kReplyTo.value = 0
    kCommentText.value = ''
    loadKAtts(k.id)
  } catch (e) { toast(e.response?.data?.error || '打开失败', 'error') }
}
function openEntry(e) { if (e && e.id) openDetailK(e) }
function editFromView() {
  if (!viewK.value) return
  const k = viewK.value
  viewK.value = null
  openEditK(k)
}
async function togglePin(k) {
  try {
    const r = await api.post('/workspace/knowledge/' + k.id + '/pin')
    k.pinned = r.pinned
    if (viewK.value && viewK.value.id === k.id) viewK.value = { ...viewK.value, pinned: r.pinned }
    toast(r.pinned ? '已置顶' : '已取消置顶')
  } catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function toggleStar(k) {
  try {
    const r = await api.post('/workspace/knowledge/' + k.id + '/star')
    k.starred = r.starred
    if (viewK.value && viewK.value.id === k.id) viewK.value = { ...viewK.value, starred: r.starred }
  } catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function removeK(k) {
  if (!confirm(`删除知识条目「${k.title}」？将进入回收站（附件保留，可恢复）。`)) return
  try { await api.del('/workspace/knowledge/' + k.id); toast('已移入回收站'); await refreshKnowledge() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

// ---- 统计看板 ----
const stats = ref(null)
const statsLoading = ref(false)
async function loadStats() {
  statsLoading.value = true
  try { stats.value = await api.get('/workspace/knowledge/stats') } catch (e) { toast(e.response?.data?.error || '统计加载失败', 'error') }
  finally { statsLoading.value = false }
}
function toggleStats() { if (kbView.value === 'stats') kbView.value = 'list'; else { kbView.value = 'stats'; loadStats() } }
function barPct(count, list) {
  const max = Math.max(1, ...list.map((x) => x.count))
  return (count / max * 100).toFixed(0) + '%'
}
function tagSize(count, list) {
  const max = Math.max(1, ...list.map((x) => x.count))
  return 12 + Math.round((count / max) * 8)
}

// ---- 回收站 ----
const trash = ref([])
async function loadTrash() {
  try { trash.value = await api.get('/workspace/knowledge/trash') } catch (e) { toast(e.response?.data?.error || '回收站加载失败', 'error') }
}
function openTrash() { if (kbView.value === 'trash') kbView.value = 'list'; else { kbView.value = 'trash'; loadTrash() } }
async function restoreK(id) {
  try { await api.post('/workspace/knowledge/' + id + '/restore'); toast('已恢复'); await Promise.all([loadTrash(), refreshKnowledge()]) }
  catch (e) { toast(e.response?.data?.error || '恢复失败', 'error') }
}
async function purgeK(id) {
  if (!confirm('彻底删除？此操作不可恢复，附件/评论/版本将一并清除。')) return
  try { await api.del('/workspace/knowledge/' + id + '/purge'); toast('已彻底删除'); await loadTrash() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}
async function emptyTrash() {
  if (!confirm('清空回收站？全部条目将彻底删除且不可恢复。')) return
  try { await api.post('/workspace/knowledge/trash/empty'); toast('已清空回收站'); await loadTrash() }
  catch (e) { toast(e.response?.data?.error || '清空失败', 'error') }
}

// ---- 模板 ----
const templates = ref([])
const templateModal = ref(false)
async function loadTemplates() {
  try { templates.value = await api.get('/workspace/knowledge/templates') } catch (e) { toast(e.response?.data?.error || '模板加载失败', 'error') }
}
function openTemplates() { templateModal.value = true; loadTemplates() }
async function applyTemplate(t) {
  try {
    const r = await api.get('/workspace/knowledge/templates/' + t.id)
    Object.assign(kForm, { id: 0, title: r.title || t.title, category: r.category || '', content: r.content || '', scope: 'department', tags: parseTags(r.tags || t.tags), parent_id: 0, status: 'published' })
    kTagDraft.value = ''
    editingK.value = true
    templateModal.value = false
    kAtts.value = []
    pendingAtts.value = []
  } catch (e) { toast(e.response?.data?.error || '套用失败', 'error') }
}
async function deleteTemplate(t) {
  if (t.builtin) { toast('系统预置模板不可删除', 'error'); return }
  if (!confirm('删除该模板？')) return
  try { await api.del('/workspace/knowledge/templates/' + t.id); toast('已删除'); await loadTemplates() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

// ---- Markdown 导入 ----
const importModal = ref(false)
const importText = ref('')
const importScope = ref('department')
const importCategory = ref('')
const importing = ref(false)
function openImport() { importModal.value = true; importText.value = ''; importScope.value = 'department'; importCategory.value = '' }
async function doImport() {
  const md = importText.value.trim()
  if (!md) { toast('请粘贴 Markdown 内容', 'error'); return }
  importing.value = true
  try {
    const r = await api.post('/workspace/knowledge/import', { markdown: md, scope: importScope.value, category: importCategory.value })
    toast('已导入 ' + (r.created || 0) + ' 条')
    importModal.value = false
    await refreshKnowledge()
  } catch (e) { toast(e.response?.data?.error || '导入失败', 'error') }
  finally { importing.value = false }
}

// ---- 导出（Markdown / Word / 原 TXT） ----
function kbQueryString() {
  const p = new URLSearchParams()
  if (kQuery.value.trim()) p.set('kw', kQuery.value.trim())
  if (kCategory.value) p.set('category', kCategory.value)
  if (kTag.value) p.set('tag', kTag.value)
  if (kMine.value) p.set('mine', '1')
  if (kStarred.value) p.set('starred', '1')
  if (kParent.value) p.set('parent', kParent.value)
  return p.toString()
}
function downloadFile(path, q, filename) {
  const tok = localStorage.getItem('sw_token')
  fetch(baseUrl + path + (q ? '?' + q : ''), { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then((r) => { if (!r.ok) throw new Error(); return r.blob() })
    .then((b) => { const u = URL.createObjectURL(b); const a = document.createElement('a'); a.href = u; a.download = filename; a.click(); a.remove(); URL.revokeObjectURL(u) })
    .catch(() => toast('导出失败', 'error'))
}
function exportMarkdown() { downloadFile('/workspace/knowledge/export/markdown', kbQueryString(), '知识库.md') }
function exportDoc() { downloadFile('/workspace/knowledge/export/doc', kbQueryString(), '知识库.doc') }

// ---- 详情子标签：评论 / 版本 / 双链 ----
const kTab = ref('content')
const kComments = ref([])
const kCommentText = ref('')
const kCommentBusy = ref(false)
const kReplyTo = ref(0)
const kVersions = ref([])
const kOutlinks = ref([])
const kBacklinks = ref([])
const topComments = computed(() => kComments.value.filter((c) => !c.parent_id))
function repliesOf(id) { return kComments.value.filter((c) => c.parent_id === id) }
async function loadComments(id) {
  try { kComments.value = await api.get('/workspace/knowledge/' + id + '/comments') } catch (e) { toast(e.response?.data?.error || '评论加载失败', 'error') }
}
async function addComment() {
  const text = kCommentText.value.trim()
  if (!text) return
  kCommentBusy.value = true
  try {
    await api.post('/workspace/knowledge/' + viewK.value.id + '/comments', { content: text, parent_id: kReplyTo.value || 0 })
    kCommentText.value = ''; kReplyTo.value = 0
    await loadComments(viewK.value.id)
    toast('评论已发表')
  } catch (e) { toast(e.response?.data?.error || '评论失败', 'error') }
  finally { kCommentBusy.value = false }
}
async function delComment(cid) {
  if (!confirm('删除该评论？')) return
  try { await api.del('/workspace/knowledge/comments/' + cid); await loadComments(viewK.value.id) } catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}
async function loadVersions(id) {
  try { kVersions.value = await api.get('/workspace/knowledge/' + id + '/versions') } catch (e) { toast(e.response?.data?.error || '版本加载失败', 'error') }
}
async function restoreVersion(vid) {
  if (!confirm('回滚到该版本？当前内容将变为所选版本。')) return
  try {
    const r = await api.post('/workspace/knowledge/' + viewK.value.id + '/version/' + vid + '/restore')
    toast('已回滚到该版本')
    if (r && r.content !== undefined) viewK.value = { ...viewK.value, ...r }
  } catch (e) { toast(e.response?.data?.error || '回滚失败', 'error') }
}
async function loadLinks(id) {
  try {
    const [o, b] = await Promise.all([api.get('/workspace/knowledge/' + id + '/outlinks'), api.get('/workspace/knowledge/' + id + '/backlinks')])
    kOutlinks.value = o; kBacklinks.value = b
  } catch (e) { toast(e.response?.data?.error || '链接加载失败', 'error') }
}

// ---- 知识库：变更 / 协作记录（v0.8.0） ----
const kHistOpen = ref(false)
const kHist = ref([])
async function toggleKHist(k) {
  if (!k) return
  if (kHistOpen.value) { kHistOpen.value = false; return }
  kHist.value = []
  kHistOpen.value = true
  try { kHist.value = await api.get('/workspace/knowledge/' + k.id + '/history') }
  catch (e) { toast(e.response?.data?.error || '变更记录加载失败', 'error') }
}
function histActionLabel(a) {
  return { create: '创建', update: '更新', delete: '删除', attachment_upload: '上传附件', attachment_delete: '删除附件', import: '导入', version_restore: '回滚版本' }[a] || a
}

// ---- 知识库附件 ----
const kAtts = ref([])
const pendingAtts = ref([]) // 新建未保存时上传到中转缓存的附件（保存后转正）
const uploading = ref(false)
const baseUrl = (import.meta.env?.BASE_URL || '') + 'api'

async function loadKAtts(id) {
  try { kAtts.value = await api.get('/workspace/knowledge/' + id + '/attachments') }
  catch (e) { toast(e.response?.data?.error || '附件加载失败', 'error') }
}
function canEditK(k) { return k && (auth.user?.id === k.owner_id || auth.isSuper) }
function canDelAtt(a) {
  if (!viewK.value) return false
  return auth.isSuper || viewK.value.owner_id === auth.user?.id || a.owner_id === auth.user?.id
}
const thumbCache = new Map()
function attKey(a) { return (a && (a.stored_name || a.id)) || '' }
function thumbUrl(a) {
  if (thumbCache.has(attKey(a))) return thumbCache.get(attKey(a))
  const tok = localStorage.getItem('sw_token')
  fetch(baseUrl + '/workspace/knowledge_attachments/' + attKey(a) + '/download', { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then((r) => r.ok ? r.blob() : Promise.reject())
    .then((b) => { const u = URL.createObjectURL(b); thumbCache.set(attKey(a), u); if (viewK.value) viewK.value = { ...viewK.value } })
    .catch(() => {})
  return ''
}
function fetchAtt(a, isPreview) {
  const tok = localStorage.getItem('sw_token')
  return fetch(baseUrl + '/workspace/knowledge_attachments/' + attKey(a) + '/download', {
    headers: tok ? { Authorization: 'Bearer ' + tok } : {}
  }).then((r) => { if (!r.ok) throw new Error('下载失败'); return r.blob() })
}
async function downloadAtt(a) {
  try {
    const blob = await fetchAtt(a, false)
    const u = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = u; link.download = a.file_name; document.body.appendChild(link); link.click()
    link.remove(); URL.revokeObjectURL(u)
  } catch (e) { toast(e.message || '下载失败', 'error') }
}
async function previewAtt(a) {
  try {
    const blob = await fetchAtt(a, true)
    const u = URL.createObjectURL(blob)
    const win = window.open(); if (win) { win.document.write('<iframe src="' + u + '" style="width:100%;height:100%;border:0"></iframe>'); win.document.title = a.file_name }
  } catch (e) { toast(e.message || '预览失败', 'error') }
}
function isPdf(a) {
  return a && (a.mime === 'application/pdf' || (a.file_name && /\.pdf$/i.test(a.file_name)))
}
async function uploadAtts(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  if (!files.length) return
  const kid = viewK.value ? viewK.value.id : (kForm.id || 0)
  uploading.value = true
  try {
    if (kid) {
      for (const f of files) await api.upload('/workspace/knowledge/' + kid + '/attachments', f)
      toast('已上传 ' + files.length + ' 个附件')
      loadKAtts(kid)
    } else {
      for (const f of files) {
        const att = await api.upload('/workspace/temp-attachments', f)
        pendingAtts.value.push({ tempId: att.id, fileName: att.file_name, mime: att.mime, size: att.size })
      }
      toast('已暂存 ' + files.length + ' 个附件，保存后生效')
    }
  } catch (err) { toast(err.response?.data?.error || '上传失败', 'error') }
  finally { uploading.value = false }
}
function removePendingAtt(a) {
  const i = pendingAtts.value.findIndex((x) => x.tempId === a.tempId)
  if (i >= 0) pendingAtts.value.splice(i, 1)
}
async function removeAtt(a) {
  if (!confirm('删除附件「' + a.file_name + '」？')) return
  const kid = viewK.value ? viewK.value.id : (kForm.id || 0)
  try { await api.del('/workspace/knowledge_attachments/' + a.id); toast('已删除'); if (kid) loadKAtts(kid) }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}
function fmtSize(n) {
  if (!n && n !== 0) return ''
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + u[i]
}
function fileIcon(name) {
  const ext = (name.split('.').pop() || '').toLowerCase()
  const imgs = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp']
  if (imgs.includes(ext)) return '🖼'
  if (['pdf'].includes(ext)) return '📄'
  if (['doc', 'docx'].includes(ext)) return '📝'
  if (['xls', 'xlsx', 'csv'].includes(ext)) return '📊'
  if (['ppt', 'pptx'].includes(ext)) return '📽'
  if (['zip', 'rar', '7z', 'tar', 'gz'].includes(ext)) return '🗜'
  if (['mp4', 'mov', 'avi', 'mkv'].includes(ext)) return '🎬'
  return '📎'
}

// ---- 原 TXT 导出 ----
function exportK() {
  const q = kbQueryString()
  const tok = localStorage.getItem('sw_token')
  fetch(baseUrl + '/workspace/knowledge/export' + (q ? '?' + q : ''), { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then((r) => { if (!r.ok) throw new Error(); return r.blob() })
    .then((blob) => { const u = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = u; a.download = '知识库导出.txt'; a.click(); a.remove(); URL.revokeObjectURL(u) })
    .catch(() => toast('导出失败', 'error'))
}
function exportBundle() {
  const tok = localStorage.getItem('sw_token')
  const a = document.createElement('a')
  a.href = baseUrl + '/workspace/export/bundle'
  a.download = ''
  if (tok) {
    fetch(baseUrl + '/workspace/export/bundle', { headers: { Authorization: 'Bearer ' + tok } })
      .then((r) => { if (!r.ok) throw new Error(); const cd = r.headers.get('Content-Disposition') || ''; const m = /filename\*=UTF-8''([^;]+)/.exec(cd); const fn = m ? decodeURIComponent(m[1]) : ('工作台数据_' + Date.now() + '.zip'); return r.blob().then((b) => ({ b, fn })) })
      .then(({ b, fn }) => { const u = URL.createObjectURL(b); const x = document.createElement('a'); x.href = u; x.download = fn; x.click(); x.remove(); URL.revokeObjectURL(u); toast('打包已生成') })
      .catch(() => toast('导出失败', 'error'))
  } else {
    a.href = '/login'
    a.click()
  }
}

// ================= 工作日志 =================
const logs = ref([])
// 筛选：日期区间 + 关键词 + 按人 + 可见范围；默认落在"今天"
const logFrom = ref(todayStr())
const logTo = ref(todayStr())
const logKw = ref('')
const logOwner = ref('')
const logScope = ref('mine')
// 分页
const logLimit = ref(20)
const logOffset = ref(0)
const logTotal = ref(0)
// 统计
const logStats = ref(null)
const logStatsOpen = ref(false)
const logMissingOpen = ref(false)
// 附件（展开式）
const logAttachOpen = ref(0)
const logAttachList = ref([])
// 上传中标志复用知识库那一个（uploading，见上方）
const editingLog = ref(false)
const savingLog = ref(false)
const logForm = reactive({ id: 0, log_date: '', title: '', done: '', pending: '', scope: 'private' })

// 日志筛选参数（withPage=false 用于统计接口，避免把 limit 带进去）
function logParams(withPage = true) {
  const p = {}
  if (logFrom.value) p.from = logFrom.value
  if (logTo.value) p.to = logTo.value
  if (logKw.value.trim()) p.q = logKw.value.trim()
  if (logOwner.value) p.owner_id = logOwner.value
  if (logScope.value === 'mine') p.mine = 1
  if (withPage) { p.limit = logLimit.value; p.offset = logOffset.value }
  return p
}
async function loadLogStats() {
  try { logStats.value = await api.get('/workspace/logs/stats', logParams(false)) } catch { /* 统计失败不阻断列表 */ }
}
async function loadLogs() {
  try {
    logs.value = await api.get('/workspace/logs', logParams(true))
    await loadLogStats()
    // 统计接口返回的 total 与列表过滤器口径一致，直接用于分页
    logTotal.value = logStats.value?.total ?? logs.value.length
  } catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
}
function applyLogFilter() { logOffset.value = 0; logAttachOpen.value = 0; loadLogs() }
function logPage(d) {
  const next = logOffset.value + d * logLimit.value
  if (next < 0) return
  logOffset.value = next
  logAttachOpen.value = 0
  loadLogs()
}
function resetLogFilters() {
  logFrom.value = todayStr(); logTo.value = todayStr()
  logKw.value = ''; logOwner.value = ''; logScope.value = 'mine'
  applyLogFilter()
}
function setLogScope(s) { logScope.value = s; applyLogFilter() }
function openNewLog() {
  Object.assign(logForm, { id: 0, log_date: logFrom.value || todayStr(), title: '', done: '', pending: '', scope: 'private' })
  editingLog.value = true
}
function openEditLog(l) {
  Object.assign(logForm, { id: l.id, log_date: l.log_date, title: l.title, done: l.done, pending: l.pending, scope: l.scope })
  editingLog.value = true
}
function cancelEditLog() { editingLog.value = false }
async function saveLog() {
  savingLog.value = true
  try {
    if (logForm.id) { await api.put('/workspace/logs/' + logForm.id, logForm); toast('已保存') }
    else { await api.post('/workspace/logs', logForm); toast('已记录') }
    editingLog.value = false
    loadLogs()
  } catch (e) { toast(e.response?.data?.error || '保存失败', 'error') }
  finally { savingLog.value = false }
}
async function removeLog(l) {
  if (!confirm('删除这篇日志？相关附件也会一并清除。')) return
  try { await api.del('/workspace/logs/' + l.id); toast('已删除'); loadLogs() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}
function exportLogs() {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(logParams(false))) q.append(k, v)
  downloadFile('/workspace/logs/export', q.toString(), '工作日志_' + todayStr() + '.csv')
}

// ---- 日志附件 ----
async function loadLogAtts(id) {
  try { logAttachList.value = await api.get('/workspace/attachments', { module: 'log', ref_id: id }) }
  catch { logAttachList.value = [] }
}
async function toggleLogAttach(l) {
  if (logAttachOpen.value === l.id) { logAttachOpen.value = 0; return }
  logAttachOpen.value = l.id
  await loadLogAtts(l.id)
}

// ---- 日志 -> 交接单（打通原本割裂的两件事） ----
const logHvTarget = ref(null)
const logHvPicker = ref(false)
const savingLogHv = ref(false)
const logHvForm = reactive({ title: '', todo: '', assignee_ids: [], priority: 'normal', due_at: '', note: '' })
function openLogHandover(l) {
  logHvTarget.value = l
  Object.assign(logHvForm, {
    title: l.title || (l.log_date + ' 待办交接'),
    todo: l.pending || '',
    assignee_ids: [], priority: 'normal', due_at: '', note: '',
  })
  logHvPicker.value = false
  loadUsers()
}
function cancelLogHandover() { logHvTarget.value = null; logHvPicker.value = false }
async function saveLogHandover() {
  if (!logHvForm.assignee_ids.length) { toast('请至少选择一名接收人', 'error'); return }
  savingLogHv.value = true
  try {
    await api.post('/workspace/logs/' + logHvTarget.value.id + '/handover', {
      title: logHvForm.title, todo: logHvForm.todo, assignee_ids: logHvForm.assignee_ids,
      priority: logHvForm.priority, due_at: logHvForm.due_at, note: logHvForm.note,
    })
    toast('已转成交接单，接收人会收到通知')
    logHvTarget.value = null
    loadLogs()
  } catch (e) { toast(e.response?.data?.error || '转交接失败', 'error') }
  finally { savingLogHv.value = false }
}

// ---- 工作台通用附件（日志 / 交接共用） ----
async function loadHAtts(id) {
  try { hAttachList.value = await api.get('/workspace/attachments', { module: 'handover', ref_id: id }) }
  catch { hAttachList.value = [] }
}
async function uploadWSAtt(e, module, refId) {
  const f = e.target?.files?.[0]
  if (e.target) e.target.value = '' // 允许重复选择同一文件
  if (!f) return
  uploading.value = true
  try {
    await api.upload('/workspace/attachments', f, 'file', { module, ref_id: refId })
    toast('已上传')
    if (module === 'log') { await loadLogAtts(refId); await loadLogs() }
    else { await loadHAtts(refId); await loadHandovers() }
  } catch (err) { toast(err.response?.data?.error || '上传失败', 'error') }
  finally { uploading.value = false }
}
async function removeWSAtt(a, module, refId) {
  if (!confirm('删除附件「' + a.file_name + '」？')) return
  try {
    await api.del('/workspace/attachments/' + a.id)
    toast('已删除')
    if (module === 'log') { await loadLogAtts(refId); await loadLogs() }
    else { await loadHAtts(refId); await loadHandovers() }
  } catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}
function wsAttUrl(a) {
  return baseUrl + '/workspace/attachments/' + encodeURIComponent(a.stored_name || a.id) + '/download'
}
function downloadWSAtt(a) {
  const tok = localStorage.getItem('sw_token')
  fetch(wsAttUrl(a), { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then((r) => { if (!r.ok) throw new Error('下载失败'); return r.blob() })
    .then((b) => {
      const u = URL.createObjectURL(b)
      const el = document.createElement('a')
      el.href = u; el.download = a.file_name
      document.body.appendChild(el); el.click(); el.remove()
      URL.revokeObjectURL(u)
    })
    .catch(() => toast('下载失败', 'error'))
}

// ================= 交接接力 =================
const users = ref([])
const handovers = ref([])
const hScope = ref('inbox')
// 筛选
const hStatus = ref('all')
const hPriority = ref('')
const hOverdueOnly = ref(false)
const hKw = ref('')
// 分页
const hLimit = ref(20)
const hOffset = ref(0)
const hTotal = ref(0)
// 统计 / 时间线 / 附件
const hStats = ref(null)
const hTimelineOpen = ref(0)
const hTimeline = ref([])
const hAttachOpen = ref(0)
const hAttachList = ref([])
const pendingInbox = ref(0) // 未筛选的「我收到的待接手」数量，用于 Tab 角标
const editingH = ref(false)
const savingH = ref(false)
const showPicker = ref(false)
const hForm = reactive({ title: '', assignee_ids: [], from_progress: '', todo: '', priority: 'normal', due_at: '' })

// Tab 角标：优先用专门统计的未筛选数字，回退到当前列表
const inboxUnread = computed(() => pendingInbox.value || handovers.value.filter((h) => h.status === 'pending').length)

const statusLabel = (s) => ({ pending: '待接手', in_progress: '处理中', done: '已完成', returned: '已退回' }[s] || s)
const statusChip = (s) => ({ pending: 'warn', in_progress: 'accent', done: 'ok', returned: 'danger' }[s] || '')
// 处理轨迹的动作 → 中文
const handoverActionLabel = (a) => ({
  create: '发起交接', accept: '接手处理', done: '标记完成',
  return: '退回', reopen: '重新派发', urge: '催办', note: '更新备注',
}[a] || a)

function parseAssigneeNames(h) {
  if (h.assignee_names) {
    try { const a = JSON.parse(h.assignee_names); if (Array.isArray(a) && a.length) return a } catch {}
  }
  if (h.assignee_name) return [h.assignee_name]
  return []
}
function assigneeNamesText(h) { return parseAssigneeNames(h).join('、') }
function isAssigneeOf(h) {
  const me = auth.user?.id
  if (!me) return false
  if (h.assignee_id === me) return true
  if (h.assignee_ids) {
    try {
      const arr = JSON.parse(h.assignee_ids)
      if (Array.isArray(arr) && arr.includes(me)) return true
    } catch {}
  }
  return false
}
const isSenderOf = (h) => !!h && h.sender_id === auth.user?.id
function userNameOf(uid) {
  const u = users.value.find((x) => x.id === uid)
  return u ? u.name : ('#' + uid)
}
const pickableUsers = computed(() => users.value.filter((u) => !u.frozen && u.id !== auth.user?.id))
// 通用多选切换（交接表单与"日志转交接"弹窗共用）
function toggleAssigneeIn(arr, uid) {
  const i = arr.indexOf(uid)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(uid)
}
function toggleAssignee(uid) { toggleAssigneeIn(hForm.assignee_ids, uid) }

async function loadUsers() {
  try {
    const all = await api.get('/users')
    users.value = all.filter((u) => !u.frozen)
  } catch {}
}
// 交接筛选参数（带分页）
function hParams() {
  const p = { role: hScope.value, status: hStatus.value === 'all' ? 'all' : hStatus.value }
  if (hPriority.value) p.priority = hPriority.value
  if (hKw.value.trim()) p.q = hKw.value.trim()
  if (hOverdueOnly.value) p.overdue = 1
  p.limit = hLimit.value
  p.offset = hOffset.value
  return p
}
// 统计卡片只按「可见范围 + 是否只看逾期」聚合，不受状态/优先级/关键词影响，
// 否则点一个卡片后其它卡片的数字会一起塌缩，失去导航意义。
async function loadHandoverStats() {
  try {
    const p = { role: hScope.value }
    if (hOverdueOnly.value) p.overdue = 1
    hStats.value = await api.get('/workspace/handovers/stats', p)
  } catch { /* 统计失败不阻断列表 */ }
}
async function loadPendingInbox() {
  try {
    const s = await api.get('/workspace/handovers/stats', { role: 'inbox' })
    pendingInbox.value = s?.pending || 0
  } catch {}
}
async function loadHandovers() {
  try {
    // 用 http 直取以读取 X-Total-Count（后端为此专门加的响应头，供分页使用）
    const r = await http.get('/workspace/handovers', { params: hParams() })
    handovers.value = r.data
    hTotal.value = Number(r.headers['x-total-count'] || (r.data?.length ?? 0))
    await Promise.all([loadHandoverStats(), loadPendingInbox()])
  } catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
}
function applyHFilter() { hOffset.value = 0; hTimelineOpen.value = 0; hAttachOpen.value = 0; loadHandovers() }
function setHFilter(kind, val) {
  if (kind === 'status') hStatus.value = val
  if (kind === 'priority') hPriority.value = val
  applyHFilter()
}
function resetHFilters() {
  hStatus.value = 'all'; hPriority.value = ''; hOverdueOnly.value = false; hKw.value = ''
  applyHFilter()
}
function hPage(d) {
  const next = hOffset.value + d * hLimit.value
  if (next < 0) return
  hOffset.value = next
  hTimelineOpen.value = 0; hAttachOpen.value = 0
  loadHandovers()
}
function setHScope(s) { hScope.value = s; applyHFilter() }
function openNewH() {
  Object.assign(hForm, { title: '', assignee_ids: [], from_progress: '', todo: '', priority: 'normal', due_at: '' })
  showPicker.value = false
  editingH.value = true
  loadUsers()
}
function cancelEditH() { editingH.value = false; showPicker.value = false }
async function saveH() {
  if (hForm.assignee_ids.length === 0) { toast('请至少选择一名接收人', 'error'); return }
  savingH.value = true
  try {
    await api.post('/workspace/handovers', {
      title: hForm.title, from_progress: hForm.from_progress, todo: hForm.todo,
      assignee_ids: hForm.assignee_ids, priority: hForm.priority, due_at: hForm.due_at,
    })
    toast(hForm.assignee_ids.length > 1 ? `已发出，${hForm.assignee_ids.length} 位接收人会收到通知` : '交接已发出，接收人会收到通知')
    editingH.value = false
    showPicker.value = false
    loadHandovers()
  } catch (e) { toast(e.response?.data?.error || '发出失败', 'error') }
  finally { savingH.value = false }
}
async function setHStatus(h, st) {
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: st }); toast('已更新'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function promptDone(h) {
  const note = prompt(`为交接「${h.title}」填写完成说明（可留空）：`)
  if (note === null) return
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: 'done', note: note || '' }); toast('已标记完成'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function promptNote(h) {
  const note = prompt('更新当前处理进展备注：', h.note || '')
  if (note === null) return
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: 'in_progress', note }); toast('已更新'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
// 退回：必须说明原因（后端也做了校验，这里给出更友好的提示）
async function promptReturn(h) {
  const reason = prompt(`退回交接「${h.title}」，请说明原因（必填）：`, '')
  if (reason === null) return
  if (!reason.trim()) { toast('退回必须填写原因', 'error'); return }
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: 'returned', note: reason.trim() }); toast('已退回给发送人'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
// 发送人把被退回的交接重新派发
async function reopenH(h) {
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: 'pending' }); toast('已重新派发'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function urgeH(h) {
  const note = prompt(`催办交接「${h.title}」，可附一句留言（可留空）：`, '')
  if (note === null) return
  try { await api.post('/workspace/handovers/' + h.id + '/urge', { note: note || '' }); toast('已催办，接收人会收到通知'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '催办失败', 'error') }
}
async function toggleHTimeline(h) {
  if (hTimelineOpen.value === h.id) { hTimelineOpen.value = 0; return }
  hTimelineOpen.value = h.id
  try { hTimeline.value = await api.get('/workspace/handovers/' + h.id + '/events') }
  catch (e) { hTimeline.value = []; toast(e.response?.data?.error || '轨迹加载失败', 'error') }
}
async function toggleHAttach(h) {
  if (hAttachOpen.value === h.id) { hAttachOpen.value = 0; return }
  hAttachOpen.value = h.id
  await loadHAtts(h.id)
}
async function removeH(h) {
  if (!confirm('删除这条交接？处理轨迹与附件会一并清除。')) return
  try { await api.del('/workspace/handovers/' + h.id); toast('已删除'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

// 递归目录树节点组件（在 script setup 中内联声明）
const TreeNode = {
  name: 'TreeNode',
  props: { node: { type: Object, required: true }, active: { type: [String, Number], default: '' } },
  emits: ['pick'],
  template: `
    <li class="tree-li">
      <span class="tree-node" :class="{active: String(active)===String(node.id)}" @click="$emit('pick', node.id)">
        📄 {{ node.title }}<span v-if="node.children.length" class="tree-badge">{{ node.children.length }}</span>
      </span>
      <ul v-if="node.children.length" class="tree-ul sub">
        <TreeNode v-for="c in node.children" :key="c.id" :node="c" :active="active" @pick="$emit('pick', $event)" />
      </ul>
    </li>
  `
}

onMounted(() => {
  refreshKnowledge()
  loadLogs()
  loadHandovers()
  if (auth.isSuper || auth.canManage) loadUsers()
  useAutoRefresh(loadK, true)
  document.addEventListener('click', onKbDocClick)
})
onUnmounted(() => {
  useAutoRefresh(loadK, false)
  document.removeEventListener('click', onKbDocClick)
})

// 关键词/按人筛选需要显式触发（applyLogFilter）；日期用 @change 触发，
// 所以这里不再需要单独的 watch（原 watch(viewDate) 已随筛选栏重构移除）
watch(tab, (t) => { if (t === 'handover') loadHandovers(); if (t === 'logs') loadLogs(); if (t === 'knowledge') refreshKnowledge() })
</script>

<style scoped>
.page-title { font-size: 20px; font-weight: 700; margin: 0; }
.ws-head { display: flex; align-items: flex-end; justify-content: space-between; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.ws-tool { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.export-bundle { white-space: nowrap; font-size: 12.5px; padding: 8px 14px; color: var(--accent); border-color: rgba(79,70,229,0.3); }
.export-bundle:hover { background: var(--accent-soft); }
.panel { padding: 18px; border-radius: 16px; background: var(--glass); border: 1px solid var(--glass-border); margin-bottom: 18px; }
.panel-head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
.head-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.section-title { font-size: 15px; font-weight: 600; margin: 0; }
.section-sub { font-size: 12px; color: var(--text-faint); font-weight: 400; }

/* 顶部 Tab */
.tabs { display: inline-flex; background: var(--overlay); border: 1px solid var(--glass-border); border-radius: 12px; padding: 3px; gap: 2px; }
.tab { border: none; background: transparent; color: var(--text-dim); padding: 8px 16px; border-radius: 9px; cursor: pointer; font-size: 13px; white-space: nowrap; }
.tab.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.handover-badge { font-style: normal; background: var(--danger); color: #fff; border-radius: 999px; font-size: 11px; padding: 0 6px; margin-left: 3px; }

/* 通用按钮/表单 */
.btn { padding: 8px 16px; border-radius: 11px; border: 1px solid var(--glass-border); background: var(--overlay-2); color: var(--text); cursor: pointer; font-size: 13px; }
.btn.primary { background: var(--brand-grad, var(--accent)); color: #fff; border: none; font-weight: 600; }
.btn.ghost { background: transparent; }
.btn.sm { padding: 6px 12px; font-size: 12px; }
.btn.ok { background: rgba(5,150,105,0.12); color: var(--ok); border-color: rgba(5,150,105,0.35); }
.btn.danger { color: var(--danger); border-color: rgba(225,29,72,0.35); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }
.btn.ghost.sm.on, .kb-actions .btn.ghost.sm.on { background: var(--accent-soft); color: var(--accent); border-color: rgba(79,70,229,0.3); }
.kb-actions { gap: 6px; }
/* 视图切换分段控件容器（分段按钮本体复用全局 .seg 样式） */
.seg-group { display: inline-flex; border: 1px solid var(--glass-border); border-radius: 10px; overflow: hidden; }
/* 组与组之间的细竖分隔线，比纯空白更能表达分组关系 */
.kb-div { width: 1px; align-self: stretch; margin: 2px 5px; background: var(--glass-border); }
/* 导出下拉：把 TXT/MD/Word 三种格式收纳起来 */
.kb-menu-wrap { position: relative; display: inline-flex; }
.kb-menu-wrap .caret { font-style: normal; font-size: 10px; opacity: 0.7; }
.kb-menu {
  position: absolute; right: 0; top: calc(100% + 6px); z-index: 40;
  min-width: 180px; padding: 6px; display: flex; flex-direction: column; gap: 2px;
  background: var(--glass-strong, rgba(255, 255, 255, 0.96));
  border: 1px solid var(--glass-border); border-radius: 12px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.16);
}
.kb-menu .menu-item {
  display: flex; align-items: center; gap: 10px; padding: 8px 12px;
  border: 0; background: transparent; border-radius: 8px; cursor: pointer;
  font-size: 13px; color: var(--text); text-align: left; font-family: inherit;
}
.kb-menu .menu-item:hover { background: var(--overlay); }
.kb-menu .mi-ico { font-size: 14px; line-height: 1; opacity: 0.85; }
.pop-enter-active, .pop-leave-active { transition: opacity 0.12s ease, transform 0.12s ease; }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(-4px); }
.edit-form { border: 1px dashed var(--glass-border-strong); border-radius: 14px; padding: 16px; margin: 4px 0 16px; background: var(--overlay); display: flex; flex-direction: column; gap: 12px; }
.fld { display: block; font-size: 12px; color: var(--text-dim); margin-bottom: 6px; }
.glass-input { width: 100%; padding: 9px 12px; border-radius: 11px; background: var(--bg-1); border: 1px solid var(--glass-border); color: var(--text); font-size: 13.5px; outline: none; }
.glass-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
textarea.ta { resize: vertical; line-height: 1.6; }
.fg2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.fg2.bot { align-items: end; }
.form-actions { display: flex; gap: 8px; justify-content: flex-end; }
.form-actions.right { justify-content: flex-end; }
.chip { display: inline-block; font-size: 11px; padding: 2px 9px; border-radius: 999px; background: var(--overlay-2); color: var(--text-dim); }
.chip.accent { background: var(--accent-soft); border-color: rgba(79,70,229,0.3); color: var(--accent); }
.chip.warn { background: rgba(217,119,6,0.14); color: #b45309; }
.chip.pick-chip { display: inline-flex; align-items: center; gap: 6px; background: var(--accent-soft, #eef2ff); color: var(--accent, #4f46e5); padding: 4px 6px 4px 10px; border-radius: 999px; font-size: 12.5px; }
.chip.pick-chip .x { background: transparent; border: 0; color: inherit; font-size: 14px; line-height: 1; cursor: pointer; padding: 0 4px; opacity: .6; }
.chip.pick-chip .x:hover { opacity: 1; }
.dim { color: var(--text-faint); font-weight: 400; }
.empty { text-align: center; color: var(--text-faint); padding: 36px 0; font-size: 13px; }
.ops { display: inline-flex; gap: 6px; }
.del { padding: 4px 10px; border-radius: 8px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-faint); cursor: pointer; font-size: 12px; }
.del:hover { color: var(--text); border-color: var(--glass-border-strong); }
.del.danger:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }
.scope-switch select { width: auto; min-width: 150px; }
.chk.mine { display: inline-flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--text-dim); cursor: pointer; white-space: nowrap; }
.chk.mine input { width: 15px; height: 15px; }

/* 标签编辑 */
.tag-edit { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.tag-input { width: auto; flex: 1; min-width: 160px; }

/* 知识库：筛选 + 卡片 */
.filter-bar { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-bottom: 14px; }
.filter-bar .search { flex: 1; min-width: 200px; }
.filter-bar select { width: auto; }
.k-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
.k-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 13px 14px; background: var(--bg-1); display: flex; flex-direction: column; gap: 8px; transition: transform .12s ease, box-shadow .12s ease; cursor: pointer; }
.k-card:hover { transform: translateY(-2px); box-shadow: 0 6px 18px rgba(15,23,42,0.08); }
.k-card.mine { border-left: 3px solid var(--accent); }
.k-card.pinned { border-left: 3px solid #d97706; }
.k-top { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.pin-badge { font-size: 12px; }
.k-owner { margin-left: auto; font-size: 11.5px; color: var(--text-faint); }
.k-title { font-size: 14px; font-weight: 700; color: var(--text); line-height: 1.4; }
.k-title :deep(mark), .k-preview :deep(mark) { background: #fde68a; color: inherit; border-radius: 3px; padding: 0 1px; }
.k-preview { font-size: 12.5px; color: var(--text-dim); line-height: 1.6; white-space: pre-wrap; word-break: break-word; max-height: 4.2em; overflow: hidden; }
.k-tags { display: flex; flex-wrap: wrap; gap: 5px; }
.k-tag { font-size: 11px; color: var(--accent); background: var(--accent-soft); border-radius: 999px; padding: 1px 8px; cursor: pointer; }
.k-foot { display: flex; align-items: center; justify-content: space-between; margin-top: auto; font-size: 11.5px; }
.k-foot-ops { display: inline-flex; align-items: center; gap: 4px; }
.icon-btn { border: none; background: transparent; cursor: pointer; font-size: 15px; line-height: 1; padding: 2px 4px; border-radius: 6px; color: var(--text-faint); }
.icon-btn.on { color: #d97706; }
.icon-btn:hover { background: var(--overlay-2); }
.icon-btn.star.on { color: #d97706; }
.icon-btn.pin.on { color: #d97706; }

/* 目录树 */
.k-tree { border: 1px dashed var(--glass-border); border-radius: 12px; padding: 8px 12px; margin-bottom: 14px; background: var(--overlay); }
.k-tree-head { display: flex; align-items: center; justify-content: space-between; font-size: 12.5px; }
.tree-ul { list-style: none; margin: 6px 0 0; padding-left: 14px; }
.tree-ul.sub { padding-left: 16px; border-left: 1px dashed var(--glass-border); margin-left: 4px; }
.tree-li { margin: 2px 0; }
.tree-node { display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--text-dim); cursor: pointer; padding: 2px 6px; border-radius: 7px; }
.tree-node:hover { background: var(--overlay-2); color: var(--text); }
.tree-node.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.tree-badge { font-size: 10px; background: var(--overlay-2); border-radius: 999px; padding: 0 5px; color: var(--text-faint); }

/* 统计看板 */
.kb-stats { display: flex; flex-direction: column; gap: 16px; }
.stat-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.stat-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 16px; background: var(--bg-1); text-align: center; }
.stat-num { font-size: 26px; font-weight: 800; color: var(--accent); }
.stat-lbl { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.stat-block { border: 1px solid var(--glass-border); border-radius: 14px; padding: 14px; background: var(--bg-1); }
.stat-h { font-size: 13px; font-weight: 600; margin-bottom: 10px; }
.bar-chart { display: flex; flex-direction: column; gap: 7px; }
.bar-row { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.bar-name { width: 110px; flex-shrink: 0; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar-track { flex: 1; height: 12px; background: var(--overlay-2); border-radius: 999px; overflow: hidden; }
.bar-fill { display: block; height: 100%; background: var(--accent); border-radius: 999px; }
.bar-cnt { width: 28px; text-align: right; color: var(--text-faint); }
.tag-cloud { display: flex; flex-wrap: wrap; gap: 8px; }
.tag-pill { display: inline-flex; align-items: center; gap: 4px; padding: 3px 10px; border-radius: 999px; background: var(--accent-soft); color: var(--accent); cursor: pointer; font-size: 13px; }
.tag-pill i { font-style: normal; font-size: 11px; opacity: .7; }
.author-list { display: flex; flex-wrap: wrap; gap: 8px; }
.author-pill { display: inline-flex; align-items: center; gap: 4px; padding: 3px 10px; border-radius: 999px; background: var(--overlay-2); color: var(--text-dim); font-size: 12.5px; }
.author-pill i { font-style: normal; opacity: .6; }

/* 回收站 */
.kb-trash { display: flex; flex-direction: column; gap: 10px; }
.trash-list { display: flex; flex-direction: column; gap: 10px; }
.trash-item { display: flex; align-items: center; justify-content: space-between; gap: 10px; border: 1px solid var(--glass-border); border-radius: 12px; padding: 12px 14px; background: var(--bg-1); flex-wrap: wrap; }
.trash-main { display: flex; flex-direction: column; gap: 3px; }
.trash-title { font-weight: 600; font-size: 14px; }
.trash-ops { display: flex; gap: 8px; }

/* 模板 / 导入 弹窗列表 */
.tpl-item, .trash-item { }
.tpl-item { display: flex; align-items: center; justify-content: space-between; gap: 10px; border: 1px solid var(--glass-border); border-radius: 12px; padding: 12px 14px; background: var(--bg-1); }
.tpl-main { display: flex; flex-direction: column; gap: 3px; }
.tpl-title { font-weight: 600; font-size: 14px; }
.tpl-ops { display: flex; gap: 8px; }

/* 详情子标签 */
.k-tabs { display: flex; gap: 4px; margin: 12px 0 6px; border-bottom: 1px solid var(--hairline); flex-wrap: wrap; }
.k-tab { border: none; background: transparent; color: var(--text-dim); padding: 7px 12px; border-radius: 9px 9px 0 0; cursor: pointer; font-size: 12.5px; }
.k-tab.on { color: var(--accent); font-weight: 600; border-bottom: 2px solid var(--accent); }

/* 评论 */
.k-comments { display: flex; flex-direction: column; gap: 10px; }
.comment { border: 1px solid var(--glass-border); border-radius: 10px; padding: 9px 11px; background: var(--overlay); }
.comment.reply { margin-left: 22px; background: var(--overlay-2); }
.comment-head { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.comment-body { font-size: 13px; line-height: 1.7; margin: 5px 0; white-space: pre-wrap; word-break: break-word; }
.comment-body :deep(code) { background: var(--overlay-2); padding: 1px 5px; border-radius: 4px; }
.comment-ops { display: flex; gap: 8px; }
.comment-editor { margin-top: 6px; }
.comment-send { display: flex; justify-content: flex-end; gap: 8px; margin-top: 6px; }

/* 版本 */
.k-versions { display: flex; flex-direction: column; gap: 10px; }
.version-item { border: 1px solid var(--glass-border); border-radius: 10px; padding: 9px 11px; background: var(--overlay); }
.version-head { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.version-meta { font-size: 12px; margin: 4px 0 8px; }

/* 双向链接 */
.k-links { display: flex; flex-direction: column; gap: 14px; }
.link-group { display: flex; flex-direction: column; gap: 6px; }
.link-h { font-size: 13px; font-weight: 600; }
.link-item { font-size: 13px; color: var(--accent); cursor: pointer; padding: 6px 10px; border: 1px solid var(--glass-border); border-radius: 9px; background: var(--bg-1); }
.link-item:hover { background: var(--accent-soft); }

/* 日志 */
.log-view-toggle, .sub-tabs { display: inline-flex; border: 1px solid var(--glass-border); border-radius: 10px; overflow: hidden; }
.seg { padding: 6px 13px; font-size: 12.5px; border: none; background: transparent; color: var(--text-dim); cursor: pointer; }
.seg + .seg { border-left: 1px solid var(--glass-border); }
.seg.on { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.date-input { width: auto; }
.log-list { display: flex; flex-direction: column; gap: 12px; }
.log-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 13px 15px; background: var(--bg-1); }
.log-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.log-time { font-size: 12px; color: var(--text-faint); font-weight: 600; }
.log-title { font-size: 14px; font-weight: 700; color: var(--text); }
.log-head .owner { margin-left: auto; }
.log-sec { font-size: 13px; line-height: 1.7; color: var(--text); margin-top: 10px; white-space: pre-wrap; }
.log-sec.done { border-left: 3px solid var(--ok); padding-left: 10px; }
.log-sec.pend { border-left: 3px solid var(--warn); padding-left: 10px; }
.sec-label { display: inline-block; font-size: 11px; color: var(--text-faint); margin-right: 8px; font-weight: 600; letter-spacing: .3px; }

/* 交接 */
.h-list { display: flex; flex-direction: column; gap: 12px; }
.h-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 14px 16px; background: var(--bg-1); }
.h-card.st-done { background: var(--overlay); }
.h-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.h-title { font-size: 14px; font-weight: 700; color: var(--text); flex: 1; min-width: 160px; }
.h-head .ops { margin-left: auto; }
.h-meta { font-size: 12px; margin-top: 7px; }
.h-meta b { color: var(--text-dim); }
.h-prog, .h-todo, .h-note { font-size: 13px; line-height: 1.7; margin-top: 9px; white-space: pre-wrap; border-radius: 10px; padding: 8px 11px; background: var(--overlay); }
.h-prog { border-left: 3px solid var(--accent); }
.h-todo { border-left: 3px solid var(--warn); }
.h-actions, .h-actor-edit { margin-top: 11px; display: flex; gap: 8px; flex-wrap: wrap; }

@media (max-width: 820px) {
  .fg2 { grid-template-columns: 1fr; }
  .ws-head { flex-direction: column; align-items: flex-start; }
  .tabs { width: 100%; }
  .tab { flex: 1; padding: 10px 6px; text-align: center; }
  .k-grid { grid-template-columns: 1fr; }
  .stat-grid { grid-template-columns: 1fr; }
  .panel-head { flex-direction: column; align-items: flex-start; }
  .scope-switch select, .date-input { width: 100%; }
  .bar-name { width: 80px; }
}

/* 知识详情弹窗 */
.modal-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; padding: 20px; }
.modal { background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: 16px; width: min(560px, 100%); max-height: 82vh; display: flex; flex-direction: column; overflow: hidden; }
.modal.sm { width: min(460px, 100%); }
.modal-head { display: flex; align-items: center; gap: 6px; padding: 14px 16px 0; }
.modal-close { margin-left: auto; border: none; background: transparent; color: var(--text-faint); font-size: 16px; cursor: pointer; }
.modal-close:hover { color: var(--text); }
.modal-title { font-size: 16px; font-weight: 700; margin: 10px 16px 4px; }
.modal-meta { font-size: 12px; margin: 0 16px 12px; }
.modal-scroll { flex: 1; overflow-y: auto; padding: 0 16px; }
.modal-body { font-size: 13.5px; line-height: 1.8; color: var(--text); white-space: pre-wrap; word-break: break-word; }
.modal-body.rte-content { white-space: normal; }
.modal-body.rte-content :deep(h2) { font-size: 17px; font-weight: 700; margin: 14px 0 6px; }
.modal-body.rte-content :deep(h3) { font-size: 15px; font-weight: 700; margin: 10px 0 4px; }
.modal-body.rte-content :deep(p) { margin: 6px 0; }
.modal-body.rte-content :deep(ul), .modal-body.rte-content :deep(ol) { padding-left: 22px; margin: 6px 0; }
.modal-body.rte-content :deep(blockquote) { border-left: 3px solid var(--accent); padding-left: 10px; color: var(--text-dim); margin: 8px 0; }
.modal-body.rte-content :deep(a) { color: var(--accent); text-decoration: underline; }
.modal-body.rte-content :deep(img) { max-width: 100%; height: auto; border-radius: 6px; margin: 6px 0; }
.modal-body.rte-content :deep(hr) { border: 0; border-top: 1px dashed var(--glass-border); margin: 12px 0; }
.modal-body.rte-content :deep(pre) { background: var(--overlay-2); padding: 8px 10px; border-radius: 8px; font-size: 12.5px; overflow-x: auto; white-space: pre; }
.modal-body.rte-content :deep(code) { background: var(--overlay-2); padding: 1px 5px; border-radius: 4px; font-size: 12.5px; }
.modal-body.rte-content :deep(table) { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 13px; }
.modal-body.rte-content :deep(th), .modal-body.rte-content :deep(td) { border: 1px solid var(--glass-border); padding: 5px 9px; vertical-align: top; text-align: left; }
.modal-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 16px; border-top: 1px solid var(--hairline); margin-top: 12px; flex-shrink: 0; }

/* 接收人多选 */
.multi-pick { display: flex; flex-direction: column; gap: 8px; }
.multi-pick .chips { display: flex; flex-wrap: wrap; gap: 6px; }
.multi-pick .picker-list { max-height: 220px; overflow: auto; border: 1px solid var(--hairline); border-radius: 8px; padding: 6px 8px; background: var(--bg-soft, rgba(255,255,255,.6)); }
.multi-pick .picker-row { display: flex; align-items: center; gap: 8px; padding: 4px 2px; cursor: pointer; }
.multi-pick .picker-row:hover { background: var(--hover, rgba(0,0,0,.04)); }
.multi-pick .picker-name { font-size: 13px; }
.multi-pick .picker-empty { padding: 8px 4px; font-size: 12.5px; }

/* 编辑表单内嵌附件区 */
.edit-attach { border: 1px dashed var(--hairline); border-radius: 10px; padding: 10px 12px; background: var(--bg-soft, rgba(255,255,255,.45)); }
.edit-attach .att-hint { margin: 4px 0 8px; font-size: 12px; }

/* 附件区 */
.modal-attach { margin-top: 14px; border-top: 1px solid var(--hairline); padding-top: 12px; }
.att-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.att-title { font-size: 13px; font-weight: 600; color: var(--text); }
.att-upload { display: inline-flex; align-items: center; gap: 8px; }
.att-upload .btn { margin: 0; display: inline-flex; align-items: center; gap: 5px; }
.up-txt { font-size: 12px; }
.att-hint { font-size: 11.5px; margin: 6px 0 10px; }
.att-list { display: flex; flex-direction: column; gap: 7px; max-height: 240px; overflow-y: auto; padding-right: 2px; }
.att-item { display: flex; align-items: center; gap: 10px; border: 1px solid var(--glass-border); border-radius: 10px; padding: 7px 10px; background: var(--overlay); }
.att-thumb { width: 40px; height: 40px; object-fit: cover; border-radius: 7px; cursor: pointer; flex-shrink: 0; background: var(--overlay-2); }
.att-ico { width: 40px; height: 40px; display: flex; align-items: center; justify-content: center; font-size: 20px; flex-shrink: 0; background: var(--overlay-2); border-radius: 7px; cursor: pointer; }
.att-name { flex: 1; min-width: 0; font-size: 12.5px; color: var(--text); cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.att-name:hover { color: var(--accent); text-decoration: underline; }
.att-size { font-size: 11px; flex-shrink: 0; }
.att-item .del { flex-shrink: 0; }
.att-ico.pdf { color: var(--accent, #4f46e5); }
.att-read { font-size: 11.5px; padding: 3px 10px; border-radius: 7px; border: 1px solid var(--accent, #4f46e5); background: transparent; color: var(--accent, #4f46e5); cursor: pointer; flex-shrink: 0; }
.att-read:hover { background: var(--accent-soft, rgba(79,70,229,0.12)); }
.att-empty { padding: 18px 0; }

@media (max-width: 640px) {
  .modal { width: 100%; max-height: 90vh; }
  .modal-scroll { padding: 0 14px; }
  .modal-title { margin-left: 14px; }
  .modal-meta { margin-left: 14px; margin-right: 14px; }
  .modal-head { padding-left: 14px; }
  .modal-foot { padding: 12px 14px calc(12px + env(safe-area-inset-bottom)); }
  .att-item { flex-wrap: wrap; }
  .att-name { min-width: 120px; }
  .att-list { max-height: 200px; }
}

/* 知识库：变更 / 协作记录（v0.8.0） */
.modal-history { margin-top: 14px; padding-top: 12px; border-top: 1px dashed var(--glass-border, rgba(255,255,255,0.12)); }
.hist-toggle { font-size: 12.5px; }
.hist-list { list-style: none; margin: 10px 0 4px; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.hist-item { background: var(--overlay-2, rgba(255,255,255,0.04)); border: 1px solid var(--glass-border, rgba(255,255,255,0.12)); border-radius: 10px; padding: 8px 10px; }
.hist-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 12.5px; }
.hist-act { font-size: 11px; padding: 1px 7px; }
.hist-detail { margin: 6px 0 0; white-space: pre-wrap; word-break: break-word; font-family: inherit; font-size: 12.5px; color: var(--text-dim, rgba(255,255,255,0.65)); line-height: 1.6; }
/* ===== v0.16.0 工作日志 / 交接接力 补齐 ===== */

/* 筛选栏 */
.ws-filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 12px 0 4px; }
.ws-filters .ff { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-faint); }
.ws-filters .glass-input { padding: 6px 9px; font-size: 12.5px; }
.ws-filters .ff-grow { flex: 1 1 180px; min-width: 150px; }
.ws-filters .ff-owner { width: auto; min-width: 110px; }
.ws-filters .ff.chk { cursor: pointer; user-select: none; }
.ws-filters .ff.chk input { cursor: pointer; }
.ws-filters .sub-tabs { margin-left: auto; }

/* 统计看板 */
.ws-stats { display: flex; align-items: stretch; gap: 10px; flex-wrap: wrap; margin: 12px 0 2px; }
.ws-stat { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; min-width: 92px; padding: 9px 14px; border: 1px solid var(--glass-border); border-radius: 12px; background: var(--bg-1); }
.ws-stat b { font-size: 20px; font-weight: 800; color: var(--accent); line-height: 1.15; }
.ws-stat span { font-size: 11.5px; color: var(--text-faint); }
.ws-stat.warn b { color: #b45309; }
.ws-missing { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin: 8px 0 2px; font-size: 12.5px; }

/* 小徽标补充 */
.chip.tiny { font-size: 10.5px; padding: 1px 7px; }
.chip.ok { background: rgba(22,163,74,0.14); color: #15803d; }
.chip.danger { background: rgba(220,38,38,0.14); color: #b91c1c; }
.chip.miss { background: rgba(217,119,6,0.12); color: #b45309; margin-right: 4px; }

/* 附件区（日志 / 交接共用） */
.attach-box { margin-top: 10px; padding-top: 10px; border-top: 1px dashed var(--hairline); }
.attach-list { display: flex; flex-direction: column; gap: 6px; margin-bottom: 8px; }
.attach-row { display: flex; align-items: center; gap: 9px; font-size: 12.5px; padding: 4px 2px; }
.attach-row .attach-name { cursor: pointer; }
.attach-row .dim { font-size: 11.5px; }
.attach-row .del { margin-left: auto; }
.file-btn { position: relative; overflow: hidden; }
.file-btn input[type=file] { position: absolute; inset: 0; opacity: 0; cursor: pointer; }

/* 分页 */
.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 14px; font-size: 12.5px; }

/* 交接统计卡片（可点击筛选） */
.stat-cards { display: flex; gap: 10px; flex-wrap: wrap; margin: 12px 0 2px; }
.stat-card { border: 1px solid var(--glass-border); border-radius: 12px; padding: 10px 16px; background: var(--bg-1); display: flex; flex-direction: column; align-items: center; gap: 2px; cursor: pointer; min-width: 86px; }
.stat-card b { font-size: 19px; font-weight: 800; line-height: 1.15; }
.stat-card span { font-size: 11.5px; color: var(--text-faint); }
.stat-card:hover { border-color: var(--accent); }
.stat-card.on { border-color: var(--accent); background: var(--accent-soft); }
.stat-card.warn b { color: #b45309; }
.stat-card.accent b { color: var(--accent); }
.stat-card.ok b { color: #15803d; }
.stat-card.danger b { color: #b91c1c; }

/* 处理轨迹时间线 */
.timeline { margin-top: 10px; padding-top: 10px; border-top: 1px dashed var(--hairline); }
.tl-list { display: flex; flex-direction: column; gap: 7px; }
.tl-item { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; font-size: 12.5px; }
.tl-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--text-faint); flex-shrink: 0; align-self: center; }
.tl-urgent .tl-dot, .tl-urge .tl-dot { background: #d97706; }
.tl-done .tl-dot { background: #16a34a; }
.tl-return .tl-dot { background: #dc2626; }
.tl-act { font-weight: 600; }
.tl-who { color: var(--accent); }
.tl-time { font-size: 11.5px; }
.tl-note { flex-basis: 100%; padding-left: 15px; color: var(--text-dim); }

/* 退回原因 */
.h-note.ret-note { background: rgba(220,38,38,0.08); border-left: 3px solid #dc2626; padding-left: 8px; border-radius: 4px; }

/* 优先级与截止时间并排 */
.pri-row { display: flex; gap: 8px; }
.pri-row .glass-input { flex: 1 1 0; min-width: 0; }

/* 转交接弹窗内部表单 */
.hv-form { display: flex; flex-direction: column; gap: 12px; padding-bottom: 4px; }

@media (max-width: 640px) {
  .ws-filters .sub-tabs { margin-left: 0; }
  .stat-card { min-width: 72px; padding: 8px 12px; }
  .ws-stat { min-width: 78px; padding: 8px 10px; }
  .pri-row { flex-direction: column; }
}
</style>
