import { error } from '@sveltejs/kit';
import { defaultDoc, docs } from '../../lib/docs/catalog';
import type { PageLoad } from './$types';

export const load: PageLoad = () => {
  if (!defaultDoc) {
    error(404, 'No documentation found');
  }

  return {
    docs,
    selectedDoc: defaultDoc,
    selectedSlug: defaultDoc.slug
  };
};
