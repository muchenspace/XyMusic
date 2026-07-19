<script setup lang="ts" generic="TData">
import { FlexRender, getCoreRowModel, useVueTable, type ColumnDef } from "@tanstack/vue-table";

const props = withDefaults(defineProps<{
  data: TData[];
  columns: ColumnDef<TData, unknown>[];
  rowKey?: (row: TData) => string;
}>(), { rowKey: undefined });
const emit = defineEmits<{ rowClick: [row: TData] }>();

const table = useVueTable({
  get data() { return props.data; },
  get columns() { return props.columns; },
  getCoreRowModel: getCoreRowModel(),
});
</script>

<template>
  <div class="overflow-x-auto">
    <table class="data-table">
      <thead>
        <tr v-for="headerGroup in table.getHeaderGroups()" :key="headerGroup.id">
          <th v-for="header in headerGroup.headers" :key="header.id" :style="{ width: header.getSize() !== 150 ? `${header.getSize()}px` : undefined }">
            <FlexRender v-if="!header.isPlaceholder" :render="header.column.columnDef.header" :props="header.getContext()" />
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in table.getRowModel().rows" :key="rowKey?.(row.original) ?? row.id" tabindex="0" @click="emit('rowClick', row.original)" @keydown.enter="emit('rowClick', row.original)">
          <td v-for="cell in row.getVisibleCells()" :key="cell.id">
            <FlexRender :render="cell.column.columnDef.cell" :props="cell.getContext()" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
