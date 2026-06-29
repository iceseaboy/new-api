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
import { Tabs, TabPane, Spin } from '@douyinfe/semi-ui';
import { API } from '../../helpers';
import MarkdownRenderer from '../../components/common/markdown/MarkdownRenderer';

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

  return (
    <div className='max-w-4xl mx-auto py-8 px-4 sm:px-6 lg:px-8'>
      <div className='bg-white rounded-lg shadow-sm p-6 sm:p-8'>
        <MarkdownRenderer content={content} />
      </div>
    </div>
  );
};

// API 文档页：复用平台布局（PageLayout 的 HeaderBar/SiderBar）与样式，
// Tabs 在「视频生成」「素材库管理」两份文档间切换。
const Doc = () => {
  const { t } = useTranslation();

  return (
    <div className='classic-page-fill bg-gray-50'>
      <Tabs
        type='line'
        defaultActiveKey='video'
        lazyRender
        tabBarStyle={{ justifyContent: 'center', paddingTop: '0.5rem' }}
      >
        <TabPane tab={t('视频生成 API')} itemKey='video'>
          <DocContent
            endpoint='/api/doc/seedance-video'
            cacheKey='doc_seedance_video'
          />
        </TabPane>
        <TabPane tab={t('素材库管理 API')} itemKey='asset'>
          <DocContent
            endpoint='/api/doc/seedance-asset'
            cacheKey='doc_seedance_asset'
          />
        </TabPane>
      </Tabs>
    </div>
  );
};

export default Doc;
