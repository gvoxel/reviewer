import type { ViewOps } from '../../api/vt'

interface FKItem {
  id: number
  title: string
}

interface FKListApi<T extends FKItem, S = unknown> {
  get: (params: { search?: S; viewOps?: ViewOps }) => Promise<T[]>
}

const PAGE_SIZE = 500
const MAX_PAGES = 100

export async function loadAllFKOptions<T extends FKItem, S = unknown>(
  api: FKListApi<T, S>,
  search?: S,
): Promise<FKItem[]> {
  const items: FKItem[] = []

  for (let page = 1; page <= MAX_PAGES; page++) {
    const batch = await api.get({
      search,
      viewOps: {
        page,
        pageSize: PAGE_SIZE,
        sortColumn: 'title',
        sortDesc: false,
      },
    })

    const list = batch ?? []
    items.push(...list.map(item => ({ id: item.id, title: item.title })))

    if (list.length < PAGE_SIZE) break
  }

  return items
}
