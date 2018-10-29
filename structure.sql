-- MySQL dump 10.13  Distrib 5.6.33, for debian-linux-gnu (x86_64)
--
-- Host: localhost    Database: indigo
-- ------------------------------------------------------
-- Server version	5.6.33-0ubuntu0.14.04.1

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!40101 SET NAMES utf8 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Table structure for table `admin_notifications`
--

DROP TABLE IF EXISTS `admin_notifications`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `admin_notifications` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `reason` int(11) NOT NULL,
  `post` int(11) NOT NULL,
  `type` tinyint(1) NOT NULL,
  `user` int(11) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `notif_read` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `bans`
--

DROP TABLE IF EXISTS `bans`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `bans` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `user` int(11) NOT NULL,
  `ip` varchar(42) NOT NULL,
  `cidr` tinyint(1) NOT NULL,
  `until` datetime NOT NULL,
  PRIMARY KEY (`id`),
  KEY `bans_ibfk_1` (`user`),
  CONSTRAINT `bans_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `blocks`
--

DROP TABLE IF EXISTS `blocks`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `blocks` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `source` int(11) NOT NULL,
  `target` int(11) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `source` (`source`,`target`),
  KEY `blocks_ibfk_2` (`target`),
  CONSTRAINT `blocks_ibfk_1` FOREIGN KEY (`source`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `blocks_ibfk_2` FOREIGN KEY (`target`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `comments`
--

DROP TABLE IF EXISTS `comments`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `comments` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `created_by` int(11) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `edited_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `feeling` tinyint(1) NOT NULL DEFAULT '0',
  `post` int(11) NOT NULL,
  `body` varchar(2000) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `image` tinytext COLLATE utf8mb4_bin,
  `url` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `is_spoiler` tinyint(1) NOT NULL DEFAULT '0',
  `post_type` tinyint(1) DEFAULT '0',
  `is_rm` tinyint(1) NOT NULL DEFAULT '0',
  `is_rm_by_admin` tinyint(1) NOT NULL DEFAULT '0',
  `attachment_type` tinyint(1) NOT NULL DEFAULT '0',
  `pinned` tinyint(1) NOT NULL DEFAULT '0',
  `url_type` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `created_by` (`created_by`),
  KEY `post` (`post`),
  CONSTRAINT `comments_ibfk_1` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `comments_ibfk_2` FOREIGN KEY (`post`) REFERENCES `posts` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `communities`
--

DROP TABLE IF EXISTS `communities`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `communities` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `title` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `description` varchar(2000) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `icon` tinytext COLLATE utf8mb4_bin NOT NULL,
  `banner` tinytext COLLATE utf8mb4_bin NOT NULL,
  `is_featured` tinyint(1) NOT NULL,
  `permissions` tinyint(1) NOT NULL DEFAULT '0',
  `rm` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `community_favorites`
--

DROP TABLE IF EXISTS `community_favorites`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `community_favorites` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `community` int(11) NOT NULL,
  `favorite_by` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `community_favorites_ibfk_1` (`community`),
  KEY `community_favorites_ibfk_2` (`favorite_by`),
  CONSTRAINT `community_favorites_ibfk_1` FOREIGN KEY (`community`) REFERENCES `communities` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `community_favorites_ibfk_2` FOREIGN KEY (`favorite_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `conversations`
--

DROP TABLE IF EXISTS `conversations`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `conversations` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `source` int(11) NOT NULL,
  `target` int(11) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `is_rm` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `conversations_ibfk_1` (`source`),
  CONSTRAINT `conversations_ibfk_1` FOREIGN KEY (`source`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `emotes`
--

DROP TABLE IF EXISTS `emotes`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `emotes` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `name` text COLLATE utf8mb4_bin NOT NULL,
  `image` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `follows`
--

DROP TABLE IF EXISTS `follows`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `follows` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `follow_to` int(11) NOT NULL,
  `follow_by` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `follow_to` (`follow_to`,`follow_by`),
  KEY `follow_by` (`follow_by`),
  CONSTRAINT `follows_ibfk_1` FOREIGN KEY (`follow_to`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `follows_ibfk_2` FOREIGN KEY (`follow_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `friend_requests`
--

DROP TABLE IF EXISTS `friend_requests`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `friend_requests` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `request_to` int(11) NOT NULL,
  `request_by` int(11) NOT NULL,
  `message` text NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `request_read` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `request_to` (`request_to`,`request_by`),
  KEY `request_by` (`request_by`),
  CONSTRAINT `friend_requests_ibfk_1` FOREIGN KEY (`request_to`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `friend_requests_ibfk_2` FOREIGN KEY (`request_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `friendships`
--

DROP TABLE IF EXISTS `friendships`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `friendships` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `source` int(11) NOT NULL,
  `target` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `source` (`source`,`target`),
  KEY `target` (`target`),
  CONSTRAINT `friendships_ibfk_1` FOREIGN KEY (`target`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `friendships_ibfk_2` FOREIGN KEY (`source`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `group_members`
--

DROP TABLE IF EXISTS `group_members`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `group_members` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `user` int(11) NOT NULL,
  `conversation` int(11) unsigned NOT NULL,
  `unread_messages` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `group_members_ibfk_1` (`user`),
  KEY `group_members_ibfk_2` (`conversation`),
  CONSTRAINT `group_members_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `group_members_ibfk_2` FOREIGN KEY (`conversation`) REFERENCES `conversations` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `images`
--

DROP TABLE IF EXISTS `images`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `images` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `value` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `hash` char(32) COLLATE utf8mb4_bin NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `imports`
--

DROP TABLE IF EXISTS `imports`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `imports` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `user` int(11) NOT NULL,
  `migration` int(11) NOT NULL,
  `username` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  PRIMARY KEY (`id`),
  KEY `imports_ibfk_1` (`user`),
  KEY `imports_ibfk_2` (`migration`),
  CONSTRAINT `imports_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `imports_ibfk_2` FOREIGN KEY (`migration`) REFERENCES `migrations` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `login_tokens`
--

DROP TABLE IF EXISTS `login_tokens`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `login_tokens` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `value` varchar(16) COLLATE utf8mb4_bin NOT NULL,
  `user` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `login_tokens_ibfk_1` (`user`),
  CONSTRAINT `login_tokens_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `messages`
--

DROP TABLE IF EXISTS `messages`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `messages` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `created_by` int(11) NOT NULL,
  `conversation_id` int(11) unsigned NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `feeling` tinyint(1) NOT NULL DEFAULT '0',
  `body` varchar(2000) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `image` tinytext NOT NULL,
  `attachment_type` tinyint(1) NOT NULL DEFAULT '0',
  `url` varchar(1024) NOT NULL,
  `url_type` tinyint(1) NOT NULL DEFAULT '0',
  `post_type` tinyint(1) NOT NULL DEFAULT '0',
  `is_rm` tinyint(1) NOT NULL DEFAULT '0',
  `msg_read` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `created_by` (`created_by`),
  KEY `messages_ibfk_2` (`conversation_id`),
  CONSTRAINT `messages_ibfk_1` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `messages_ibfk_2` FOREIGN KEY (`conversation_id`) REFERENCES `conversations` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `migrated_communities`
--

DROP TABLE IF EXISTS `migrated_communities`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `migrated_communities` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `migrated_id` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `icon` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `title` text COLLATE utf8mb4_bin NOT NULL,
  `migration` int(11) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `migrations`
--

DROP TABLE IF EXISTS `migrations`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `migrations` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `image` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `script` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `url` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `is_rm` tinyint(1) NOT NULL DEFAULT '0',
  `password_required` tinyint(1) NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `notifications`
--

DROP TABLE IF EXISTS `notifications`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `notifications` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `notif_type` tinyint(1) NOT NULL,
  `notif_by` int(11) DEFAULT NULL,
  `notif_to` int(11) NOT NULL,
  `notif_post` int(11) DEFAULT NULL,
  `merged` int(11) DEFAULT NULL,
  `notif_date` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `notif_read` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `options`
--

DROP TABLE IF EXISTS `options`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `options` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `post` int(11) NOT NULL,
  `name` varchar(64) COLLATE utf8mb4_bin DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `options_ibfk_1` (`post`),
  CONSTRAINT `options_ibfk_1` FOREIGN KEY (`post`) REFERENCES `posts` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `password_resets`
--

DROP TABLE IF EXISTS `password_resets`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `password_resets` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `token` varchar(16) COLLATE utf8mb4_bin NOT NULL,
  `user` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `password_resets_ibfk_1` (`user`),
  CONSTRAINT `password_resets_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `posts`
--

DROP TABLE IF EXISTS `posts`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `posts` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `created_by` int(11) NOT NULL,
  `community_id` int(11) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `edited_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `feeling` tinyint(1) NOT NULL DEFAULT '0',
  `body` varchar(2000) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `image` tinytext COLLATE utf8mb4_bin,
  `attachment_type` tinyint(1) NOT NULL DEFAULT '0',
  `url` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `is_spoiler` tinyint(1) NOT NULL DEFAULT '0',
  `is_rm` tinyint(1) NOT NULL DEFAULT '0',
  `is_rm_by_admin` tinyint(1) NOT NULL DEFAULT '0',
  `post_type` tinyint(1) NOT NULL DEFAULT '0',
  `migration` int(11) NOT NULL DEFAULT '0',
  `import_id` int(11) NOT NULL DEFAULT '0',
  `migrated_id` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `migrated_community` varchar(32) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `pinned` tinyint(1) NOT NULL DEFAULT '0',
  `privacy` tinyint(1) NOT NULL DEFAULT '0',
  `url_type` tinyint(1) NOT NULL DEFAULT '0',
  `repost` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `created_by` (`created_by`),
  KEY `community_id` (`community_id`),
  CONSTRAINT `posts_ibfk_1` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `posts_ibfk_2` FOREIGN KEY (`community_id`) REFERENCES `communities` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `profiles`
--

DROP TABLE IF EXISTS `profiles`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `profiles` (
  `user` int(11) NOT NULL AUTO_INCREMENT,
  `comment` text COLLATE utf8mb4_bin NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `nnid` varchar(16) COLLATE utf8mb4_bin NOT NULL,
  `mh` varchar(13) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `region` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `gender` int(1) NOT NULL,
  `nnid_visibility` tinyint(1) NOT NULL,
  `yeah_visibility` tinyint(1) NOT NULL,
  `reply_visibility` tinyint(1) NOT NULL,
  `discord` varchar(37) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `steam` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `psn` varchar(16) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `switch_code` varchar(17) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `twitter` varchar(15) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `youtube` varchar(1024) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `allow_friend` tinyint(1) NOT NULL DEFAULT '1',
  `favorite` int(11) NOT NULL DEFAULT '0',
  `avatar_image` varchar(1024) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `avatar_id` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user`),
  CONSTRAINT `profiles_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `reports`
--

DROP TABLE IF EXISTS `reports`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `reports` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `type` tinyint(1) NOT NULL,
  `pid` int(11) NOT NULL,
  `message` varchar(100) COLLATE utf8mb4_bin NOT NULL,
  `user` int(11) NOT NULL,
  `reason` int(11) NOT NULL,
  `is_rm` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `reports_ibfk_1` (`user`),
  CONSTRAINT `reports_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `roles`
--

DROP TABLE IF EXISTS `roles`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `roles` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `image` varchar(1024) COLLATE utf8mb4_bin NOT NULL,
  `organization` varchar(32) COLLATE utf8mb4_bin NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `sessions`
--

DROP TABLE IF EXISTS `sessions`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `sessions` (
  `id` text COLLATE utf8mb4_bin NOT NULL,
  `user` int(11) NOT NULL,
  KEY `sessions_ibfk_1` (`user`),
  CONSTRAINT `sessions_ibfk_1` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `users`
--

DROP TABLE IF EXISTS `users`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `users` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `username` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `nickname` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci NOT NULL,
  `avatar` tinytext COLLATE utf8mb4_bin NOT NULL,
  `email` tinytext COLLATE utf8mb4_bin,
  `password` varchar(75) COLLATE utf8mb4_bin NOT NULL,
  `ip` varchar(39) COLLATE utf8mb4_bin NOT NULL,
  `level` int(2) NOT NULL,
  `role` int(11) NOT NULL,
  `online` tinyint(1) NOT NULL DEFAULT '0',
  `last_seen` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `hide_last_seen` tinyint(1) NOT NULL DEFAULT '0',
  `color` varchar(7) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `theme` varchar(31) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `yeah_notifications` tinyint(1) NOT NULL,
  `has_mh` tinyint(1) NOT NULL DEFAULT '0',
  `hide_online` tinyint(1) NOT NULL DEFAULT '0',
  `group_permissions` tinyint(1) NOT NULL DEFAULT '0',
  `websockets_enabled` tinyint(1) NOT NULL DEFAULT '1',
  `forbidden_keywords` longtext COLLATE utf8mb4_bin NOT NULL,
  `default_privacy` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `votes`
--

DROP TABLE IF EXISTS `votes`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `votes` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `option_id` int(11) NOT NULL,
  `user` int(11) NOT NULL,
  `poll` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `votes_ibfk_1` (`poll`),
  KEY `votes_ibfk_2` (`user`),
  CONSTRAINT `votes_ibfk_2` FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `yeahs`
--

DROP TABLE IF EXISTS `yeahs`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `yeahs` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `yeah_post` int(11) NOT NULL,
  `yeah_by` int(11) NOT NULL,
  `on_comment` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `yeah_post` (`yeah_post`,`yeah_by`,`on_comment`),
  KEY `yeah_by` (`yeah_by`),
  CONSTRAINT `yeahs_ibfk_1` FOREIGN KEY (`yeah_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
/*!40101 SET character_set_client = @saved_cs_client */;
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2018-10-29 13:05:13
