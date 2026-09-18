import { expect, it } from 'vitest'
import { folderOptions } from './folders'

it('disambiguates repeated folder names with their ancestor paths', () => {
  const f = (id: string, name: string, parent_id: string | null) => ({ id, name, parent_id, created_at: '', updated_at: '' })
  expect(folderOptions([f('a', 'Work', null), f('b', 'Home', null), f('c', 'Shell', 'a'), f('d', 'Shell', 'b')]))
    .toEqual([{ id: 'b', path: 'Home' }, { id: 'd', path: 'Home / Shell' }, { id: 'a', path: 'Work' }, { id: 'c', path: 'Work / Shell' }])
})
