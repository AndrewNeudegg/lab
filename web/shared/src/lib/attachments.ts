import type { HomelabdTaskAttachment } from './types';

export const MAX_DASHBOARD_ATTACHMENTS = 8;
export const MAX_DASHBOARD_ATTACHMENT_BYTES = 6 * 1024 * 1024;
export const MAX_DASHBOARD_ATTACHMENT_TEXT = 128 * 1024;

const textLikeTypes = ['json', 'xml', 'yaml', 'javascript', 'typescript', 'css', 'html', 'markdown'];

export const attachmentID = () => `att_${Date.now().toString(36)}_${Math.random().toString(16).slice(2, 10)}`;

export const formatAttachmentSize = (bytes = 0) => {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

export const isImageAttachment = (attachment: Pick<HomelabdTaskAttachment, 'content_type'>) =>
  attachment.content_type.toLowerCase().startsWith('image/');

export const textAttachment = (
  name: string,
  contentType: string,
  text: string
): HomelabdTaskAttachment => ({
  id: attachmentID(),
  name,
  content_type: contentType,
  size: new Blob([text]).size,
  text,
  created_at: new Date().toISOString()
});

export const dataUrlAttachment = (
  name: string,
  contentType: string,
  dataUrl: string,
  size = Math.round((dataUrl.length * 3) / 4)
): HomelabdTaskAttachment => ({
  id: attachmentID(),
  name,
  content_type: contentType,
  size,
  data_url: dataUrl,
  created_at: new Date().toISOString()
});

const shouldReadTextPreview = (file: File) => {
  const type = file.type.toLowerCase();
  return type.startsWith('text/') || textLikeTypes.some((candidate) => type.includes(candidate));
};

const readFileAsDataURL = (file: File) =>
  new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ''));
    reader.onerror = () => reject(reader.error || new Error('Unable to read file.'));
    reader.readAsDataURL(file);
  });

export const fileToTaskAttachment = async (file: File): Promise<HomelabdTaskAttachment> => {
  if (file.size > MAX_DASHBOARD_ATTACHMENT_BYTES) {
    throw new Error(`${file.name} is larger than ${formatAttachmentSize(MAX_DASHBOARD_ATTACHMENT_BYTES)}.`);
  }
  const attachment = dataUrlAttachment(
    file.name || 'attachment',
    file.type || 'application/octet-stream',
    await readFileAsDataURL(file),
    file.size
  );
  if (shouldReadTextPreview(file)) {
    const text = await file.text();
    attachment.text =
      text.length > MAX_DASHBOARD_ATTACHMENT_TEXT
        ? `${text.slice(0, MAX_DASHBOARD_ATTACHMENT_TEXT)}\n\n[truncated]`
        : text;
  }
  return attachment;
};
