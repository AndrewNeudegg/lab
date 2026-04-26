import { error } from '@sveltejs/kit';
import { docs, getDocBySlug } from '../../../lib/docs/catalog';
import type { PageLoad } from './$types';

export const load: PageLoad = ({ params }) => {
  const selectedDoc = getDocBySlug(params.slug);

  if (!selectedDoc) {
    error(404, 'Documentation page not found');
  }

  return {
    docs,
    selectedDoc,
    selectedSlug: selectedDoc.slug
  };
};
