/*
Copyright (C) 2023-2026 QuantumNous

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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Markdown } from '@/components/ui/markdown'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

interface DocEntry {
  key: string
  title: string
}

// 文档清单：key 对应后端 /api/doc/:key 路由（router/doc-router.go）
const DOC_ENTRIES: DocEntry[] = [
  { key: 'seedance-video', title: 'Seedance 视频生成' },
  { key: 'seedance-asset', title: 'Seedance 素材库' },
  { key: 'kling-video', title: 'Kling 视频生成' },
  { key: 'happyhorse-video', title: 'HappyHorse 视频生成' },
]

interface DocResponse {
  success: boolean
  data?: string
  message?: string
}

async function getDocContent(key: string) {
  const res = await api.get<DocResponse>(`/api/doc/${key}`)
  return res.data
}

export function Docs() {
  const { t } = useTranslation()
  const [activeKey, setActiveKey] = useState(DOC_ENTRIES[0].key)

  const { data, isLoading } = useQuery({
    queryKey: ['doc', activeKey],
    queryFn: () => getDocContent(activeKey),
  })

  return (
    <PublicLayout>
      <div className='mx-auto flex w-full max-w-6xl gap-6 px-4 py-8'>
        <aside className='w-48 shrink-0'>
          <nav className='sticky top-20 space-y-1'>
            {DOC_ENTRIES.map((entry) => (
              <button
                key={entry.key}
                type='button'
                onClick={() => setActiveKey(entry.key)}
                className={cn(
                  'w-full rounded-md px-3 py-2 text-left text-sm transition-colors',
                  activeKey === entry.key
                    ? 'bg-primary/10 text-primary font-medium'
                    : 'text-muted-foreground hover:bg-muted'
                )}
              >
                {t(entry.title)}
              </button>
            ))}
          </nav>
        </aside>
        <main className='min-w-0 flex-1'>
          {isLoading ? (
            <div className='space-y-4'>
              <Skeleton className='h-8 w-2/3' />
              <Skeleton className='h-4 w-full' />
              <Skeleton className='h-4 w-full' />
              <Skeleton className='h-4 w-1/2' />
            </div>
          ) : data?.success && data.data ? (
            <Markdown className='max-w-none'>{data.data}</Markdown>
          ) : (
            <p className='text-muted-foreground'>{t('文档不存在')}</p>
          )}
        </main>
      </div>
    </PublicLayout>
  )
}
