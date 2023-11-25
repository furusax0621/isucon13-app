ALTER TABLE `reactions` ADD INDEX `livestream_id_idx` (`livestream_id`);
ALTER TABLE `livestream_viewers_history` ADD INDEX `livestream_id_idx` (`livestream_id`);
ALTER TABLE `livecomments` ADD INDEX `livestream_id_idx` (`livestream_id`);
ALTER TABLE `livecomment_reports` ADD INDEX `livestream_id_idx` (`livestream_id`);
