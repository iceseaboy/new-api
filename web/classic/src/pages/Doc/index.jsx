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

import React from 'react';
import { useTranslation } from 'react-i18next';
import { Tabs, TabPane } from '@douyinfe/semi-ui';
import DocumentRenderer from '../../components/common/DocumentRenderer';

// API 文档页：复用平台布局（HeaderBar/SiderBar 由 PageLayout 提供）与样式，
// 通过 Tabs 在「视频生成」「素材库管理」两份文档间切换。
const Doc = () => {
  const { t } = useTranslation();

  return (
    <Tabs
      type='line'
      defaultActiveKey='video'
      lazyRender
      collapsible
      contentStyle={{ padding: 0 }}
      tabBarStyle={{ justifyContent: 'center' }}
    >
      <TabPane tab={t('视频生成 API')} itemKey='video'>
        <DocumentRenderer
          apiEndpoint='/api/doc/seedance-video'
          title={t('Seedance 2.0 视频生成 API')}
          cacheKey='doc_seedance_video'
          emptyMessage={t('加载文档失败')}
        />
      </TabPane>
      <TabPane tab={t('素材库管理 API')} itemKey='asset'>
        <DocumentRenderer
          apiEndpoint='/api/doc/seedance-asset'
          title={t('Seedance 素材库管理 API')}
          cacheKey='doc_seedance_asset'
          emptyMessage={t('加载文档失败')}
        />
      </TabPane>
    </Tabs>
  );
};

export default Doc;
