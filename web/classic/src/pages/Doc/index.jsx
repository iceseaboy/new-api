/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Spin } from '@douyinfe/semi-ui';
import { API } from '../../helpers';
import MarkdownRenderer from '../../components/common/markdown/MarkdownRenderer';

const DOCS = [
  {
    key: 'video',
    title: '视频生成 API',
    endpoint: '/api/doc/seedance-video',
    cacheKey: 'doc_seedance_video',
  },
  {
    key: 'asset',
    title: '素材库管理 API',
    endpoint: '/api/doc/seedance-asset',
    cacheKey: 'doc_seedance_asset',
  },
];

// 单份文档：拉取 markdown 原文并用平台 MarkdownRenderer 渲染。
// 直接走 MarkdownRenderer（而非 DocumentRenderer），避免文档中 `<API_KEY>` 等
// 占位符被 HTML 探测误判为 HTML 而整篇当纯文本输出。
const DocContent = ({ endpoint, cacheKey }) => {
  const [content, setContent] = useState(
    () => localStorage.getItem(cacheKey) || '',
  );
  const [loading, setLoading] = useState(!content);

  useEffect(() => {
    let active = true;
    setContent(localStorage.getItem(cacheKey) || '');
    API.get(endpoint)
      .then((res) => {
        const { success, data } = res.data || {};
        if (active && success && data) {
          setContent(data);
          localStorage.setItem(cacheKey, data);
        }
      })
      .catch(() => {})
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [endpoint, cacheKey]);

  if (loading && !content) {
    return (
      <div className='flex justify-center py-20'>
        <Spin size='large' />
      </div>
    );
  }

  return <MarkdownRenderer content={content} />;
};

// API 文档页：复用平台布局（PageLayout 的固定 HeaderBar），顶部留出导航高度，
// 文档切换用左侧栏（移动端为顶部横向按钮），避免被固定导航遮挡。
const Doc = () => {
  const { t } = useTranslation();
  const [activeKey, setActiveKey] = useState('video');
  const activeDoc = DOCS.find((d) => d.key === activeKey) || DOCS[0];

  const navItem = (doc, extraClass = '') => (
    <button
      key={doc.key}
      type='button'
      onClick={() => setActiveKey(doc.key)}
      className={`text-left px-3 py-2 rounded-md text-sm transition-colors ${
        doc.key === activeKey
          ? 'bg-blue-50 text-blue-600 font-medium'
          : 'text-gray-600 hover:bg-gray-100'
      } ${extraClass}`}
    >
      {t(doc.title)}
    </button>
  );

  return (
    <div className='classic-page-fill bg-gray-50 pt-[64px]'>
      <div className='max-w-7xl mx-auto flex items-start gap-6 px-4 sm:px-6 lg:px-8 py-6'>
        {/* 侧栏（桌面端），sticky 固定在导航下方 */}
        <aside className='hidden md:block w-56 flex-shrink-0 sticky top-[80px]'>
          <div className='bg-white rounded-lg shadow-sm p-3 flex flex-col gap-1'>
            <div className='px-3 py-2 text-xs font-medium text-gray-400'>
              {t('API 文档')}
            </div>
            {DOCS.map((doc) => navItem(doc))}
          </div>
        </aside>

        <main className='flex-1 min-w-0'>
          {/* 移动端：顶部横向切换 */}
          <div className='md:hidden flex gap-2 mb-4'>
            {DOCS.map((doc) => navItem(doc, 'flex-1 text-center bg-white shadow-sm'))}
          </div>

          <div className='bg-white rounded-lg shadow-sm p-6 sm:p-8'>
            <DocContent
              endpoint={activeDoc.endpoint}
              cacheKey={activeDoc.cacheKey}
            />
          </div>
        </main>
      </div>
    </div>
  );
};

export default Doc;
